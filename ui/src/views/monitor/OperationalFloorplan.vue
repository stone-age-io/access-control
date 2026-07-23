<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import L from 'leaflet'
import { useRouter } from 'vue-router'
import { pb } from '@/utils/pb'
import { useFloorPlan } from '@/composables/useFloorPlan'
import { useUIStore } from '@/stores/ui'
import { useAuthStore } from '@/stores/auth'
import { useAreaCommands } from '@/composables/useAreaCommands'
import { aggregateArm, armBadge, armLabel } from '@/utils/arming'
import type { Location, Portal, Area, PointStatus, AccessEvent } from '@/types/pocketbase'
import PortalCommandDrawer from '@/components/map/PortalCommandDrawer.vue'

const props = defineProps<{ locationId: string }>()
const router = useRouter()
const ui = useUIStore()
const auth = useAuthStore()

const canCommand = computed(() => auth.can('command'))
const { commanding, arm, disarm, armClear } = useAreaCommands()

const { initFloorPlan, renderMarkers, setSelected, invalidateSize, cleanup } = useFloorPlan()

const location = ref<Location | null>(null)
const portals = ref<Portal[]>([])
const statusByCode = ref<Map<string, PointStatus>>(new Map()) // keyed by portal code
const areas = ref<Area[]>([]) // this location's intrusion areas
const areaShadowsByCode = ref<Map<string, PointStatus[]>>(new Map()) // one row per controller
const armPanelOpen = ref(true)
const alarmingIds = ref<Set<string>>(new Set()) // portal ids flashing from a recent alarm
const selectedPortalId = ref<string | null>(null)
const loading = ref(true)
const isMobile = ref(false)
const floorplanReady = ref(false)

let unsubStatus: (() => void) | null = null
let unsubEvents: (() => void) | null = null
const flashTimers = new Map<string, ReturnType<typeof setTimeout>>()

// Floor plan vs. door-list is a user choice (persisted), but a location with no
// uploaded plan can only show the list — so the effective mode falls back to 'list'
// there and the toggle is hidden.
const hasFloorplan = computed(() => !!location.value?.floorplan)
const viewMode = computed<'plan' | 'list'>(() => (hasFloorplan.value ? ui.monitorViewMode : 'list'))

const placedPortals = computed(() => portals.value.filter(isPlaced))
const selectedPortal = computed(() => portals.value.find((p) => p.id === selectedPortalId.value) || null)
const selectedStatus = computed(() =>
  selectedPortal.value ? statusByCode.value.get(selectedPortal.value.code) ?? null : null,
)

function isPlaced(p: Portal): boolean {
  const pos = p.floorplan_position
  return !!pos && typeof pos.x === 'number' && typeof pos.y === 'number'
}

function checkMobile() {
  isMobile.value = window.innerWidth < 768
}

// The map height tracks the viewport, so a window resize changes the container —
// tell Leaflet to recompute its layout (otherwise tiles/markers go stale).
function onResize() {
  checkMobile()
  if (floorplanReady.value) invalidateSize()
}

function statusFor(p: Portal): PointStatus | undefined {
  return statusByCode.value.get(p.code)
}

// Live aggregated arm-state for an area, from its per-controller shadows.
function armFor(a: Area) {
  return aggregateArm(areaShadowsByCode.value.get(a.code) ?? [])
}

function doorBadgeFor(p: Portal): { cls: string; text: string } {
  switch (statusFor(p)?.state) {
    case 'open':
      return { cls: 'badge-error', text: 'Open' }
    case 'closed':
      return { cls: 'badge-success', text: 'Closed' }
    default:
      return { cls: 'badge-ghost', text: 'Unknown' }
  }
}

// The door list mirrors the plan's marker semantics: flash on a recent alarm and
// flag a manual posture override, so the two views are peers rather than fallbacks.
function isAlarming(p: Portal): boolean {
  return alarmingIds.value.has(p.id)
}

function isOverridden(p: Portal): boolean {
  return statusFor(p)?.posture_source === 'override'
}

// ---- marker styling (status-colored divIcon) ----
function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string,
  )
}

function iconFor(portal: Portal): L.DivIcon {
  const st = statusFor(portal)
  const cls = ['fp-dot', `fp-state-${st?.state || 'unknown'}`]
  if (st?.posture) cls.push(`fp-posture-${st.posture}`)
  if (st?.held) cls.push('fp-held')
  if (alarmingIds.value.has(portal.id)) cls.push('fp-alarm')
  // A manual override is independent of which posture it set, so flag it on the
  // label (an amber ⚠ chip) rather than the dot — an operator can scan the plan
  // for doors someone forced and never cleared, whatever colour the dot is.
  const overridden = st?.posture_source === 'override'
  const name = escapeHtml(portal.name || portal.code)
  const label = overridden ? `⚠ ${name}` : name
  return L.divIcon({
    className: 'fp-marker',
    html: `<span class="${cls.join(' ')}"></span><span class="fp-label${overridden ? ' fp-label-override' : ''}">${label}</span>`,
    iconSize: [16, 16],
    iconAnchor: [8, 8],
  })
}

// ---- data + live wiring ----
async function loadStatuses() {
  try {
    const rows = await pb.collection('point_status').getFullList<PointStatus>({ filter: 'kind = "portal"' })
    const m = new Map<string, PointStatus>()
    for (const r of rows) m.set(r.code, r)
    statusByCode.value = m
  } catch {
    statusByCode.value = new Map()
  }
}

// Areas + their arm shadows are supplementary to the door view — load them non-fatally
// so a failure here never bounces the operator off the floor plan.
async function loadAreas() {
  try {
    areas.value = await pb.collection('areas').getFullList<Area>({ filter: `location = "${props.locationId}"`, sort: 'code' })
  } catch {
    areas.value = []
  }
}

async function loadAreaShadows() {
  try {
    const rows = await pb.collection('point_status').getFullList<PointStatus>({ filter: 'kind = "area"' })
    const m = new Map<string, PointStatus[]>()
    for (const r of rows) m.set(r.code, [...(m.get(r.code) ?? []), r])
    areaShadowsByCode.value = m
  } catch {
    areaShadowsByCode.value = new Map()
  }
}

async function load() {
  loading.value = true
  selectedPortalId.value = null
  floorplanReady.value = false
  try {
    const [loc, pts] = await Promise.all([
      pb.collection('locations').getOne<Location>(props.locationId),
      pb.collection('portals').getFullList<Portal>({ filter: `location = "${props.locationId}"`, sort: 'code' }),
    ])
    location.value = loc
    portals.value = pts
    await Promise.all([loadStatuses(), loadAreas(), loadAreaShadows()])
  } catch {
    router.push('/monitor')
    return
  } finally {
    loading.value = false
  }
  await nextTick()
  // Init the plan only when it's the visible view — Leaflet must measure a shown
  // (display:block) container, so we defer init until 'plan' mode is active.
  if (viewMode.value === 'plan') loadMap()
}

function loadMap() {
  if (!location.value?.floorplan) return
  const imageUrl = pb.files.getURL(location.value, location.value.floorplan)
  const img = new Image()
  img.onload = () => {
    initFloorPlan('monitor-floorplan-container', imageUrl, img.width, img.height)
    floorplanReady.value = true
    renderAll()
  }
  img.onerror = () => {
    floorplanReady.value = false
  }
  img.src = imageUrl
}

function renderAll() {
  if (!floorplanReady.value) return
  renderMarkers(placedPortals.value, {
    draggable: false,
    onMove: () => {},
    onClick: openDrawer,
    iconFor,
  })
  setSelected(selectedPortalId.value)
}

function openDrawer(id: string) {
  selectedPortalId.value = id
  if (floorplanReady.value) {
    setSelected(id)
    if (!isMobile.value) nextTick(invalidateSize)
  }
}

function closeDrawer() {
  selectedPortalId.value = null
  if (floorplanReady.value) {
    setSelected(null)
    if (!isMobile.value) nextTick(invalidateSize)
  }
}

function handleMapBgClick(event: MouseEvent) {
  if (!selectedPortalId.value) return
  const target = event.target as HTMLElement
  if (target.closest('.leaflet-marker-icon') || target.closest('.monitor-drawer')) return
  closeDrawer()
}

function flashPortal(id: string) {
  alarmingIds.value.add(id)
  renderAll()
  const prev = flashTimers.get(id)
  if (prev) clearTimeout(prev)
  flashTimers.set(
    id,
    setTimeout(() => {
      alarmingIds.value.delete(id)
      flashTimers.delete(id)
      renderAll()
    }, 6000),
  )
}

async function subscribe() {
  // Live door/posture/held state AND area arm-state — point_status is small, so watch
  // the whole collection once and branch by kind (portals key one row per code; areas
  // key a list, one row per participating controller).
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    const r = e.record
    if (r.kind === 'portal') {
      const m = new Map(statusByCode.value)
      if (e.action === 'delete') m.delete(r.code)
      else m.set(r.code, r)
      statusByCode.value = m
      renderAll()
    } else if (r.kind === 'area') {
      const m = new Map(areaShadowsByCode.value)
      const arr = (m.get(r.code) ?? []).filter((s) => s.key !== r.key)
      if (e.action !== 'delete') arr.push(r)
      if (arr.length) m.set(r.code, arr)
      else m.delete(r.code)
      areaShadowsByCode.value = m
    }
  })
  // Forced/held alarms are events, not sticky state — flash the marker transiently.
  unsubEvents = await pb.collection('events').subscribe<AccessEvent>('*', (e) => {
    if (e.action !== 'create') return
    const ev = e.record
    if (ev.kind !== 'alarm') return
    if (location.value && ev.location !== location.value.code) return
    if ((ev.reason || '').toLowerCase().includes('clear')) return // held_clear: not an onset
    const portal = portals.value.find((p) => p.code === ev.portal)
    if (portal) flashPortal(portal.id)
  })
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', onResize)
  load()
  subscribe()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  flashTimers.forEach((t) => clearTimeout(t))
  if (unsubStatus) unsubStatus()
  if (unsubEvents) unsubEvents()
  cleanup()
})

// Switching to the plan: init it the first time it's shown (deferred from load),
// otherwise recompute Leaflet's layout — it was display:none while the list showed,
// so its cached size is stale and tiles/markers would render mispositioned.
watch(viewMode, (mode) => {
  if (mode !== 'plan') return
  nextTick(() => {
    if (floorplanReady.value) {
      invalidateSize()
      renderAll()
    } else {
      loadMap()
    }
  })
})

// Switching buildings within the monitor: tear down and reload.
watch(
  () => props.locationId,
  () => {
    cleanup()
    floorplanReady.value = false
    load()
  },
)
</script>

<template>
  <div class="flex flex-col h-full">
    <!-- Context bar -->
    <div class="flex items-center justify-between gap-3 mb-3 flex-wrap shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <router-link to="/monitor" class="btn btn-sm btn-ghost gap-1">← <span class="hidden sm:inline">All locations</span></router-link>
        <h2 v-if="location" class="font-bold text-lg truncate">{{ location.name || location.code }}</h2>
        <!-- View toggle — only meaningful when there's a plan to switch to. -->
        <div v-if="hasFloorplan" class="join shrink-0">
          <button
            class="join-item btn btn-sm"
            :class="viewMode === 'plan' ? 'btn-active btn-primary' : ''"
            @click="ui.setMonitorViewMode('plan')"
          >
            🗺️ <span class="hidden sm:inline">Floor plan</span>
          </button>
          <button
            class="join-item btn btn-sm"
            :class="viewMode === 'list' ? 'btn-active btn-primary' : ''"
            @click="ui.setMonitorViewMode('list')"
          >
            ☰ <span class="hidden sm:inline">Doors</span>
          </button>
        </div>
      </div>
      <!-- Legend -->
      <div class="flex items-center gap-3 text-xs flex-wrap">
        <span class="flex items-center gap-1"><span class="lg-dot bg-success"></span>Closed</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-error"></span>Open</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-base-300"></span>Unknown</span>
        <span class="flex items-center gap-1"><span class="lg-dot ring-2 ring-warning"></span>Held</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-error animate-pulse"></span>Alarm</span>
        <span class="flex items-center gap-1"><span class="text-warning font-bold leading-none">⚠</span>Override</span>
      </div>
    </div>

    <div v-if="loading" class="flex-1 min-h-0 flex items-center justify-center">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div v-else-if="location" class="relative isolate flex-1 min-h-0 bg-base-300 rounded-xl overflow-hidden border border-base-300">
      <!-- Floor plan — kept mounted (v-show) so flipping to the door list and back
           doesn't tear down / re-init Leaflet; the viewMode watch re-measures it. -->
      <div
        v-if="hasFloorplan"
        v-show="viewMode === 'plan'"
        id="monitor-floorplan-container"
        class="absolute inset-0 z-0"
        @click="handleMapBgClick"
      ></div>

      <!-- Door list — a peer view (always available, default when there's no plan):
           a clickable grid of live door cards. -->
      <div v-if="viewMode === 'list'" class="absolute inset-0 overflow-y-auto p-4">
        <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          <button
            v-for="p in portals"
            :key="p.id"
            class="text-left p-3 rounded-lg bg-base-100 border transition-colors flex items-center justify-between gap-2 min-w-0"
            :class="isAlarming(p) ? 'border-error ring-2 ring-error animate-pulse' : 'border-base-300 hover:border-primary/40'"
            @click="openDrawer(p.id)"
          >
            <span class="min-w-0">
              <span class="font-medium text-sm truncate block">
                <span v-if="isOverridden(p)" class="text-warning font-bold" title="Manual posture override">⚠ </span>{{ p.name || p.code }}
              </span>
              <code class="text-xs text-primary">{{ p.code }}</code>
            </span>
            <span class="flex items-center gap-1 shrink-0">
              <span v-if="statusFor(p)?.held" class="badge badge-xs badge-warning">Held</span>
              <span class="badge badge-sm" :class="doorBadgeFor(p).cls">{{ doorBadgeFor(p).text }}</span>
            </span>
          </button>
          <p v-if="portals.length === 0" class="text-sm opacity-50 col-span-full text-center py-4">
            No portals in this location.
          </p>
        </div>
      </div>

      <!-- Area arm panel — a left-anchored overlay listing this location's areas with
           live arm-state and arm/disarm (independent of plan/list view). Areas have no
           coordinates, so they're a panel, not markers. Collapsible to stay out of the way. -->
      <div v-if="areas.length" class="monitor-arm-panel absolute top-2 left-2 z-[500] w-60 max-w-[calc(100%-1rem)]">
        <div class="rounded-xl border border-base-300 bg-base-100/95 shadow-xl backdrop-blur">
          <button
            class="flex items-center justify-between w-full px-3 py-2 text-sm font-bold"
            @click="armPanelOpen = !armPanelOpen"
          >
            <span class="flex items-center gap-1">🛡️ Areas <span class="opacity-50 font-normal">({{ areas.length }})</span></span>
            <span class="opacity-60">{{ armPanelOpen ? '▾' : '▸' }}</span>
          </button>
          <div v-if="armPanelOpen" class="max-h-80 overflow-y-auto border-t border-base-200 divide-y divide-base-200">
            <div v-for="a in areas" :key="a.id" class="p-2.5 space-y-1.5">
              <div class="flex items-center justify-between gap-2">
                <span class="text-sm font-medium truncate">{{ a.name || a.code }}</span>
                <span class="badge badge-xs shrink-0" :class="armBadge(armFor(a).state)">{{ armLabel(armFor(a)) }}</span>
              </div>
              <div v-if="canCommand" class="flex gap-1">
                <button class="btn btn-xs btn-warning flex-1" :disabled="commanding" @click="arm(a.id, a.code)">Arm</button>
                <button class="btn btn-xs flex-1" :disabled="commanding" @click="disarm(a.id, a.code)">Disarm</button>
                <button class="btn btn-xs btn-ghost" :disabled="commanding || !a.arm_override" title="Clear override" @click="armClear(a.id)">✕</button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <PortalCommandDrawer
        v-if="selectedPortal"
        :portal="selectedPortal"
        :status="selectedStatus"
        :is-mobile="isMobile"
        class="monitor-drawer"
        @close="closeDrawer"
      />
    </div>
  </div>
</template>

<style scoped>
.lg-dot {
  display: inline-block;
  width: 0.6rem;
  height: 0.6rem;
  border-radius: 9999px;
}
</style>

<!-- Global (un-scoped): Leaflet injects marker HTML outside the component's
     scoped DOM, so these class names must be global to take effect. -->
<style>
.leaflet-div-icon.fp-marker {
  background: transparent;
  border: 0;
  width: auto !important;
  height: auto !important;
}
.fp-dot {
  display: inline-block;
  width: 16px;
  height: 16px;
  border-radius: 9999px;
  border: 2px solid #fff;
  box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.35);
  background: #9ca3af; /* unknown */
}
.fp-state-closed {
  background: #22c55e;
}
.fp-state-open {
  background: #f59e0b;
}
.fp-state-unknown {
  background: #9ca3af;
}
/* Posture overrides the door-state color where it matters operationally. */
.fp-posture-lockdown {
  background: #ef4444;
}
.fp-posture-disabled {
  background: #6b7280;
}
.fp-posture-unlocked {
  box-shadow: 0 0 0 2px #3b82f6;
}
.fp-held {
  box-shadow: 0 0 0 3px #f59e0b, 0 0 8px #f59e0b;
}
.fp-alarm {
  background: #ef4444 !important;
  animation: fp-pulse 1s infinite;
}
@keyframes fp-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(239, 68, 68, 0.7);
  }
  50% {
    box-shadow: 0 0 0 9px rgba(239, 68, 68, 0);
  }
}
.fp-label {
  position: absolute;
  left: 18px;
  top: -2px;
  white-space: nowrap;
  font-size: 10px;
  font-weight: 600;
  color: oklch(var(--bc));
  background: oklch(var(--b1) / 0.8);
  padding: 0 4px;
  border-radius: 4px;
}
/* A manually-overridden portal — amber label so it stands out on the plan. */
.fp-label-override {
  color: oklch(var(--wac));
  background: oklch(var(--wa) / 0.9);
  font-weight: 700;
}
</style>
