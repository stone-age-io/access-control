<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import L from 'leaflet'
import { useRouter } from 'vue-router'
import { pb } from '@/utils/pb'
import { useFloorPlan } from '@/composables/useFloorPlan'
import { useUIStore } from '@/stores/ui'
import type { SoftTone } from '@/utils/badges'
import type { Location, Portal, Area, AuxInput, AuxOutput, PointStatus, AccessEvent } from '@/types/pocketbase'
import {
  portalPlaceable, auxInputPlaceable, auxOutputPlaceable,
  isPlaced, statusKeyFor, PLACE_KIND_META,
  type Placeable,
} from '@/utils/placeable'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import PortalCommandDrawer from '@/components/map/PortalCommandDrawer.vue'
import AuxCommandDrawer from '@/components/map/AuxCommandDrawer.vue'
import AreaGrid from '@/components/map/AreaGrid.vue'
import IoGrid from '@/components/map/IoGrid.vue'

// The Live Map is a single "view switcher": Floor plan / Portals / Areas / I/O
// are peer views of the same location, one shown at a time. The slide-over
// command drawer is reserved for a single point tapped on the plan (or a portal
// card) — everything else is an in-place grid.
type MonitorView = 'plan' | 'portals' | 'areas' | 'io'

const props = defineProps<{ locationId: string }>()
const router = useRouter()
const ui = useUIStore()

const { initFloorPlan, renderMarkers, setSelected, invalidateSize, cleanup } = useFloorPlan()

const location = ref<Location | null>(null)
const portals = ref<Portal[]>([])
const auxInputs = ref<AuxInput[]>([])
const auxOutputs = ref<AuxOutput[]>([])
const statusByCode = ref<Map<string, PointStatus>>(new Map()) // portal status, keyed by portal code
const auxStatusByKey = ref<Map<string, PointStatus>>(new Map()) // aux status, keyed by point_status.key (auxin./auxout.)
const areas = ref<Area[]>([]) // this location's intrusion areas
const areaShadowsByCode = ref<Map<string, PointStatus[]>>(new Map()) // one row per controller
const selectedId = ref<string | null>(null) // namespaced marker id (portal / aux single drawer)
const view = ref<MonitorView>('plan')
const alarmingIds = ref<Set<string>>(new Set()) // portal record ids flashing from a recent alarm
const loading = ref(true)
const isMobile = ref(false)
const floorplanReady = ref(false)

let unsubStatus: (() => void) | null = null
let unsubEvents: (() => void) | null = null
let unsubAreas: (() => void) | null = null
const flashTimers = new Map<string, ReturnType<typeof setTimeout>>()

const hasFloorplan = computed(() => !!location.value?.floorplan)
const hasAux = computed(() => auxInputs.value.length > 0 || auxOutputs.value.length > 0)

// The view actually shown, clamped to what this location has (a persisted 'plan'
// with no floor plan, or an 'areas'/'io' selection carried to a location without
// them, falls back rather than showing an empty pane).
const effectiveView = computed<MonitorView>(() => {
  const v = view.value
  const fallback: MonitorView = hasFloorplan.value ? 'plan' : 'portals'
  if (v === 'plan' && !hasFloorplan.value) return 'portals'
  if (v === 'areas' && !areas.value.length) return fallback
  if (v === 'io' && !hasAux.value) return fallback
  return v
})

// Portals + aux I/O normalized into one marker list; only placed ones render.
const allPlaceables = computed<Placeable[]>(() => [
  ...portals.value.map(portalPlaceable),
  ...auxInputs.value.map(auxInputPlaceable),
  ...auxOutputs.value.map(auxOutputPlaceable),
])
const placedPlaceables = computed(() => allPlaceables.value.filter(isPlaced))
const selectedPlaceable = computed(() => allPlaceables.value.find((p) => p.id === selectedId.value) || null)

// Selected marker routed to the right drawer (portal vs aux).
const selectedPortal = computed(() =>
  selectedPlaceable.value?.kind === 'portal'
    ? portals.value.find((p) => p.id === selectedPlaceable.value!.recordId) || null
    : null,
)
const selectedPortalStatus = computed(() =>
  selectedPortal.value ? statusByCode.value.get(selectedPortal.value.code) ?? null : null,
)
const selectedAux = computed(() => {
  const pl = selectedPlaceable.value
  if (!pl || pl.kind === 'portal') return null
  const rec =
    pl.kind === 'aux_input'
      ? auxInputs.value.find((r) => r.id === pl.recordId)
      : auxOutputs.value.find((r) => r.id === pl.recordId)
  if (!rec) return null
  return { kind: pl.kind, record: rec, status: auxStatusByKey.value.get(statusKeyFor(pl.kind, rec.code)) ?? null }
})

function selectView(v: MonitorView) {
  view.value = v
  closeDrawer() // a per-item drawer from another view is stale here
  // Persist only the plan-vs-list preference; areas/io are transient selections.
  if (v === 'plan') ui.setMonitorViewMode('plan')
  else if (v === 'portals') ui.setMonitorViewMode('list')
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

function doorBadgeFor(p: Portal): { tone: SoftTone; text: string } {
  switch (statusFor(p)?.state) {
    case 'open':
      return { tone: 'error', text: 'Open' }
    case 'closed':
      return { tone: 'success', text: 'Closed' }
    default:
      return { tone: 'neutral', text: 'Unknown' }
  }
}

// The portal list mirrors the plan's marker semantics: flash on a recent alarm and
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

function iconFor(item: Placeable): L.DivIcon {
  return item.kind === 'portal' ? portalIcon(item) : auxIcon(item)
}

function portalIcon(item: Placeable): L.DivIcon {
  const st = statusByCode.value.get(item.code)
  const cls = ['fp-dot', `fp-state-${st?.state || 'unknown'}`]
  if (st?.posture) cls.push(`fp-posture-${st.posture}`)
  if (st?.held) cls.push('fp-held')
  if (alarmingIds.value.has(item.recordId)) cls.push('fp-alarm')
  // A manual override is independent of which posture it set, so flag it on the
  // label (an amber ⚠ chip) rather than the dot — an operator can scan the plan
  // for portals someone forced and never cleared, whatever colour the dot is.
  const overridden = st?.posture_source === 'override'
  const name = escapeHtml(item.name || item.code)
  const label = overridden ? `⚠ ${name}` : name
  return L.divIcon({
    className: 'fp-marker',
    html: `<span class="${cls.join(' ')}"></span><span class="fp-label${overridden ? ' fp-label-override' : ''}">${label}</span>`,
    iconSize: [16, 16],
    iconAnchor: [8, 8],
  })
}

// Aux markers are emoji pins (🔌 input / 🔆 output) so they read as aux at a
// glance (distinct from portal dots); the ring color encodes live state.
function auxIcon(item: Placeable): L.DivIcon {
  const st = auxStatusByKey.value.get(statusKeyFor(item.kind, item.code))
  const state = st?.state || 'unknown'
  const meta = PLACE_KIND_META[item.kind]
  const name = escapeHtml(item.name || item.code)
  return L.divIcon({
    className: 'fp-marker',
    html: `<span class="fp-auxpin fp-auxstate-${state}">${meta.emoji}</span><span class="fp-label">${name}</span>`,
    iconSize: [22, 22],
    iconAnchor: [11, 11],
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

async function loadAuxStatuses() {
  try {
    const rows = await pb.collection('point_status').getFullList<PointStatus>({
      filter: 'kind = "aux_input" || kind = "aux_output"',
    })
    const m = new Map<string, PointStatus>()
    for (const r of rows) m.set(r.key, r)
    auxStatusByKey.value = m
  } catch {
    auxStatusByKey.value = new Map()
  }
}

async function loadAux() {
  try {
    const [ins, outs] = await Promise.all([
      pb.collection('aux_input').getFullList<AuxInput>({ filter: `location = "${props.locationId}"`, sort: 'code' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ filter: `location = "${props.locationId}"`, sort: 'code' }),
    ])
    auxInputs.value = ins
    auxOutputs.value = outs
  } catch {
    auxInputs.value = []
    auxOutputs.value = []
  }
}

// Areas + their arm shadows are supplementary to the portal view — load them non-fatally
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
  selectedId.value = null
  floorplanReady.value = false
  try {
    const [loc, pts] = await Promise.all([
      pb.collection('locations').getOne<Location>(props.locationId),
      pb.collection('portals').getFullList<Portal>({ filter: `location = "${props.locationId}"`, sort: 'code' }),
    ])
    location.value = loc
    portals.value = pts
    // Initialize the view from the persisted plan/list preference (a location
    // with no floor plan always starts on Portals).
    view.value = loc.floorplan ? (ui.monitorViewMode === 'list' ? 'portals' : 'plan') : 'portals'
    await Promise.all([loadStatuses(), loadAuxStatuses(), loadAux(), loadAreas(), loadAreaShadows()])
  } catch {
    router.push('/monitor')
    return
  } finally {
    loading.value = false
  }
  await nextTick()
  // Init the plan only when it's the visible view — Leaflet must measure a shown
  // (display:block) container, so we defer init until 'plan' mode is active.
  if (effectiveView.value === 'plan') loadMap()
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
  renderMarkers(placedPlaceables.value, {
    draggable: false,
    onMove: () => {},
    onClick: openDrawer,
    iconFor,
  })
  setSelected(selectedId.value)
}

function openDrawer(markerId: string) {
  selectedId.value = markerId
  if (floorplanReady.value) {
    setSelected(markerId)
    if (!isMobile.value) nextTick(invalidateSize)
  }
}

function closeDrawer() {
  selectedId.value = null
  if (floorplanReady.value) {
    setSelected(null)
    if (!isMobile.value) nextTick(invalidateSize)
  }
}

function handleMapBgClick(event: MouseEvent) {
  if (!selectedId.value) return
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
  // Live portal/aux/area state — point_status is small, so watch the whole
  // collection once and branch by kind (portals key one row per code; aux by the
  // full status key; areas key a list, one row per participating controller).
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    const r = e.record
    if (r.kind === 'portal') {
      const m = new Map(statusByCode.value)
      if (e.action === 'delete') m.delete(r.code)
      else m.set(r.code, r)
      statusByCode.value = m
      renderAll()
    } else if (r.kind === 'aux_input' || r.kind === 'aux_output') {
      const m = new Map(auxStatusByKey.value)
      if (e.action === 'delete') m.delete(r.key)
      else m.set(r.key, r)
      auxStatusByKey.value = m
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
  // Area records carry the durable arm_override the Clear button gates on — a shadow
  // only carries live state, not the override field. Watch the collection so an
  // arm/disarm/clear (or entry-disarm / release sweep) keeps the Areas view live.
  unsubAreas = await pb.collection('areas').subscribe<Area>('*', (e) => {
    if (e.action !== 'update') return
    const idx = areas.value.findIndex((a) => a.id === e.record.id)
    if (idx === -1) return
    const next = areas.value.slice()
    next[idx] = e.record
    areas.value = next
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
  if (unsubAreas) unsubAreas()
  cleanup()
})

// Switching to the plan: init it the first time it's shown (deferred from load),
// otherwise recompute Leaflet's layout — it was display:none while another view
// showed, so its cached size is stale and tiles/markers would render mispositioned.
watch(effectiveView, (mode) => {
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
    <!-- Header: name on its own line, one view switcher below, legend last (plan only). -->
    <div class="mb-3 shrink-0 space-y-2">
      <div class="flex items-center gap-2 min-w-0">
        <router-link to="/monitor" class="btn btn-sm btn-ghost gap-1 px-2 shrink-0">
          ← <span class="hidden sm:inline">All locations</span>
        </router-link>
        <h2 v-if="location" class="font-bold text-lg truncate">{{ location.name || location.code }}</h2>
      </div>

      <!-- View switcher — one segmented control; full-width on mobile so it never cramps. -->
      <div class="join w-full sm:w-auto">
        <button
          v-if="hasFloorplan"
          class="join-item btn btn-sm flex-1 sm:flex-none gap-1"
          :class="effectiveView === 'plan' ? 'btn-active btn-primary' : ''"
          @click="selectView('plan')"
        >
          🗺️ <span class="hidden sm:inline">Floor plan</span>
        </button>
        <button
          class="join-item btn btn-sm flex-1 sm:flex-none gap-1"
          :class="effectiveView === 'portals' ? 'btn-active btn-primary' : ''"
          @click="selectView('portals')"
        >
          ☰ <span class="hidden sm:inline">Portals</span>
        </button>
        <button
          v-if="areas.length"
          class="join-item btn btn-sm flex-1 sm:flex-none gap-1"
          :class="effectiveView === 'areas' ? 'btn-active btn-primary' : ''"
          @click="selectView('areas')"
        >
          🛡️ <span class="hidden sm:inline">Areas</span>
        </button>
        <button
          v-if="hasAux"
          class="join-item btn btn-sm flex-1 sm:flex-none gap-1"
          :class="effectiveView === 'io' ? 'btn-active btn-primary' : ''"
          @click="selectView('io')"
        >
          🔌 <span class="hidden sm:inline">I/O</span>
        </button>
      </div>

      <!-- Legend — only meaningful over the plan's colored markers. -->
      <div v-if="effectiveView === 'plan'" class="flex items-center gap-x-3 gap-y-1 text-xs flex-wrap">
        <span class="flex items-center gap-1"><span class="lg-dot bg-success"></span>Closed</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-error"></span>Open</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-base-300"></span>Unknown</span>
        <span class="flex items-center gap-1"><span class="lg-dot ring-2 ring-warning"></span>Held</span>
        <span class="flex items-center gap-1"><span class="lg-dot bg-error animate-pulse"></span>Alarm</span>
        <span class="flex items-center gap-1"><span class="text-warning font-bold leading-none">⚠</span>Override</span>
        <span v-if="hasAux" class="flex items-center gap-1">🔌🔆 Aux I/O</span>
      </div>
    </div>

    <div v-if="loading" class="flex-1 min-h-0 flex items-center justify-center">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div v-else-if="location" class="relative isolate flex-1 min-h-0 bg-base-300 rounded-xl overflow-hidden border border-base-300">
      <!-- Floor plan — kept mounted (v-show) so switching views and back doesn't tear
           down / re-init Leaflet; the effectiveView watch re-measures it. -->
      <div
        v-if="hasFloorplan"
        v-show="effectiveView === 'plan'"
        id="monitor-floorplan-container"
        class="absolute inset-0 z-0"
        @click="handleMapBgClick"
      ></div>

      <!-- Portals — a clickable grid of live portal cards (opens the command drawer). -->
      <div v-if="effectiveView === 'portals'" class="absolute inset-0 overflow-y-auto p-4">
        <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          <button
            v-for="p in portals"
            :key="p.id"
            class="text-left p-3 rounded-lg bg-base-100 border transition-colors flex items-center justify-between gap-2 min-w-0"
            :class="isAlarming(p) ? 'border-error ring-2 ring-error animate-pulse' : 'border-base-300 hover:border-primary/40'"
            @click="openDrawer(`portal:${p.id}`)"
          >
            <span class="min-w-0">
              <span class="font-medium text-sm truncate block">
                <span v-if="isOverridden(p)" class="text-warning font-bold" title="Manual posture override">⚠ </span>{{ p.name || p.code }}
              </span>
              <code class="text-xs text-primary">{{ p.code }}</code>
            </span>
            <span class="flex items-center gap-1 shrink-0">
              <SoftBadge v-if="statusFor(p)?.held" tone="warning" dot>Held</SoftBadge>
              <SoftBadge :tone="doorBadgeFor(p).tone" dot>{{ doorBadgeFor(p).text }}</SoftBadge>
            </span>
          </button>
          <p v-if="portals.length === 0" class="text-sm opacity-50 col-span-full text-center py-4">
            No portals in this location.
          </p>
        </div>
      </div>

      <!-- Areas — arm-state card grid (peer of the Portals list). -->
      <div v-if="effectiveView === 'areas'" class="absolute inset-0 overflow-y-auto">
        <AreaGrid :areas="areas" :shadows="areaShadowsByCode" />
      </div>

      <!-- I/O — aux input/output card grid with inline output controls. -->
      <div v-if="effectiveView === 'io'" class="absolute inset-0 overflow-y-auto">
        <IoGrid :aux-inputs="auxInputs" :aux-outputs="auxOutputs" :status-by-key="auxStatusByKey" />
      </div>

      <!-- Per-item command drawer — a single point tapped on the plan (or a portal
           card). One is open at a time; slides over whatever view is showing. -->
      <PortalCommandDrawer
        v-if="selectedPortal"
        :portal="selectedPortal"
        :status="selectedPortalStatus"
        :is-mobile="isMobile"
        class="monitor-drawer"
        @close="closeDrawer"
      />
      <AuxCommandDrawer
        v-else-if="selectedAux"
        :kind="selectedAux.kind"
        :record="selectedAux.record"
        :status="selectedAux.status"
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
/* Aux markers: emoji pin with a status-colored ring. */
.fp-auxpin {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 9999px;
  font-size: 12px;
  line-height: 1;
  background: oklch(var(--b1));
  border: 2px solid #9ca3af; /* idle/unknown */
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.4);
}
.fp-auxstate-active {
  border-color: #f59e0b;
  box-shadow: 0 0 0 2px rgba(245, 158, 11, 0.45);
}
.fp-auxstate-energized {
  border-color: #22c55e;
  box-shadow: 0 0 0 2px rgba(34, 197, 94, 0.45);
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
