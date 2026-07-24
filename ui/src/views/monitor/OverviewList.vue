<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { pb } from '@/utils/pb'
import type { Location, Portal, Area, AuxInput, AuxOutput, PointStatus } from '@/types/pocketbase'
import type { SoftTone } from '@/utils/badges'
import { statusKeyFor } from '@/utils/placeable'
import { aggregateArm, rollupArmStates, armTone, type ArmState } from '@/utils/arming'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import AreaGrid from '@/components/map/AreaGrid.vue'
import IoGrid from '@/components/map/IoGrid.vue'

// The all-locations "List" mode of the Live View: every location's portals,
// areas, and aux I/O, grouped by location. Each row leads with a glanceable
// rollup (counts + open / arm / live) and expands to the live points — the same
// AreaGrid / IoGrid used per-location, plus portal state cards. A summary-first
// board that expands to detail, so it scales from one building to many.

const locations = ref<Location[]>([])
const allPortals = ref<Portal[]>([])
const allAreas = ref<Area[]>([])
const allAuxInputs = ref<AuxInput[]>([])
const allAuxOutputs = ref<AuxOutput[]>([])
const statusByCode = ref<Map<string, PointStatus>>(new Map()) // portal status by code
const auxStatusByKey = ref<Map<string, PointStatus>>(new Map()) // aux status by point_status.key
const areaShadowsByCode = ref<Map<string, PointStatus[]>>(new Map()) // area shadows by code (one per controller)
const expanded = ref<Set<string>>(new Set())
const loading = ref(true)

let unsubStatus: (() => void) | null = null
let unsubAreas: (() => void) | null = null

interface Group {
  loc: Location
  portals: Portal[]
  areas: Area[]
  auxInputs: AuxInput[]
  auxOutputs: AuxOutput[]
}

const groups = computed<Group[]>(() =>
  locations.value.map((loc) => ({
    loc,
    portals: allPortals.value.filter((p) => p.location === loc.id),
    areas: allAreas.value.filter((a) => a.location === loc.id),
    auxInputs: allAuxInputs.value.filter((a) => a.location === loc.id),
    auxOutputs: allAuxOutputs.value.filter((a) => a.location === loc.id),
  })),
)

const allExpanded = computed(() => groups.value.length > 0 && groups.value.every((g) => expanded.value.has(g.loc.id)))

function isExpanded(id: string) {
  return expanded.value.has(id)
}
function toggle(id: string) {
  const next = new Set(expanded.value)
  next.has(id) ? next.delete(id) : next.add(id)
  expanded.value = next
}
function toggleAll() {
  expanded.value = allExpanded.value ? new Set() : new Set(groups.value.map((g) => g.loc.id))
}

// ---- rollups ----
function portalsOpen(ps: Portal[]): number {
  return ps.filter((p) => statusByCode.value.get(p.code)?.state === 'open').length
}
function areaRollup(ars: Area[]): ArmState {
  return rollupArmStates(ars.map((a) => aggregateArm(areaShadowsByCode.value.get(a.code) ?? []).state))
}
function ioLive(g: Group): number {
  const active = g.auxInputs.filter((a) => auxStatusByKey.value.get(statusKeyFor('aux_input', a.code))?.state === 'active').length
  const on = g.auxOutputs.filter((a) => auxStatusByKey.value.get(statusKeyFor('aux_output', a.code))?.state === 'energized').length
  return active + on
}
function armLabelShort(state: ArmState): string {
  return state === 'armed' ? 'Armed' : state === 'disarmed' ? 'Disarmed' : state === 'partial' ? 'Mixed' : '—'
}
function portalBadge(p: Portal): { tone: SoftTone; text: string } {
  switch (statusByCode.value.get(p.code)?.state) {
    case 'open':
      return { tone: 'error', text: 'Open' }
    case 'closed':
      return { tone: 'success', text: 'Closed' }
    default:
      return { tone: 'neutral', text: 'Unknown' }
  }
}

// ---- data + live wiring ----
function indexStatuses(rows: PointStatus[]) {
  const byCode = new Map<string, PointStatus>()
  const byKey = new Map<string, PointStatus>()
  const areaByCode = new Map<string, PointStatus[]>()
  for (const r of rows) {
    if (r.kind === 'portal') byCode.set(r.code, r)
    else if (r.kind === 'aux_input' || r.kind === 'aux_output') byKey.set(r.key, r)
    else if (r.kind === 'area') areaByCode.set(r.code, [...(areaByCode.get(r.code) ?? []), r])
  }
  statusByCode.value = byCode
  auxStatusByKey.value = byKey
  areaShadowsByCode.value = areaByCode
}

async function load() {
  loading.value = true
  try {
    const [locs, ps, ars, ins, outs, statuses] = await Promise.all([
      pb.collection('locations').getFullList<Location>({ sort: 'code' }),
      pb.collection('portals').getFullList<Portal>({ sort: 'code' }),
      pb.collection('areas').getFullList<Area>({ sort: 'code' }),
      pb.collection('aux_input').getFullList<AuxInput>({ sort: 'code' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ sort: 'code' }),
      pb.collection('point_status').getFullList<PointStatus>(),
    ])
    locations.value = locs
    allPortals.value = ps
    allAreas.value = ars
    allAuxInputs.value = ins
    allAuxOutputs.value = outs
    indexStatuses(statuses)
  } catch {
    // Leave whatever loaded; the board degrades to what it has.
  } finally {
    loading.value = false
  }
}

async function subscribe() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    const r = e.record
    if (r.kind === 'portal') {
      const m = new Map(statusByCode.value)
      if (e.action === 'delete') m.delete(r.code)
      else m.set(r.code, r)
      statusByCode.value = m
    } else if (r.kind === 'aux_input' || r.kind === 'aux_output') {
      const m = new Map(auxStatusByKey.value)
      if (e.action === 'delete') m.delete(r.key)
      else m.set(r.key, r)
      auxStatusByKey.value = m
    } else if (r.kind === 'area') {
      const m = new Map(areaShadowsByCode.value)
      const arr = (m.get(r.code) ?? []).filter((s) => s.key !== r.key)
      if (e.action !== 'delete') arr.push(r)
      if (arr.length) m.set(r.code, arr)
      else m.delete(r.code)
      areaShadowsByCode.value = m
    }
  })
  // Area record updates carry arm_override (the AreaGrid Clear button gates on it).
  unsubAreas = await pb.collection('areas').subscribe<Area>('*', (e) => {
    if (e.action !== 'update') return
    const idx = allAreas.value.findIndex((a) => a.id === e.record.id)
    if (idx === -1) return
    const next = allAreas.value.slice()
    next[idx] = e.record
    allAreas.value = next
  })
}

onMounted(() => {
  load()
  subscribe()
})
onBeforeUnmount(() => {
  if (unsubStatus) unsubStatus()
  if (unsubAreas) unsubAreas()
})
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="flex items-center justify-between mb-3">
      <p class="text-sm opacity-60">{{ groups.length }} location{{ groups.length === 1 ? '' : 's' }}</p>
      <button v-if="groups.length" class="btn btn-xs btn-ghost" @click="toggleAll">
        {{ allExpanded ? 'Collapse all' : 'Expand all' }}
      </button>
    </div>

    <div v-if="loading" class="flex justify-center py-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div v-else class="space-y-2">
      <div v-for="g in groups" :key="g.loc.id" class="rounded-xl border border-base-300 bg-base-100 overflow-hidden">
        <!-- Header row: a toggle button + a drill-in link (siblings, never nested). -->
        <div class="flex items-center gap-2 p-3">
          <button class="flex items-center gap-3 flex-1 min-w-0 text-left" @click="toggle(g.loc.id)">
            <span class="text-base-content/40 text-xs transition-transform shrink-0" :class="isExpanded(g.loc.id) ? 'rotate-90' : ''">▶</span>
            <span class="font-semibold truncate">{{ g.loc.name || g.loc.code }}</span>
            <span class="flex items-center gap-1.5 flex-wrap ml-auto justify-end">
              <SoftBadge v-if="g.portals.length" tone="neutral" dot>
                🚪 {{ g.portals.length }}<template v-if="portalsOpen(g.portals)"> · {{ portalsOpen(g.portals) }} open</template>
              </SoftBadge>
              <SoftBadge v-if="g.areas.length" :tone="armTone(areaRollup(g.areas))" dot>
                🛡️ {{ armLabelShort(areaRollup(g.areas)) }}
              </SoftBadge>
              <SoftBadge v-if="g.auxInputs.length || g.auxOutputs.length" tone="neutral" dot>
                🔌 {{ g.auxInputs.length + g.auxOutputs.length }}<template v-if="ioLive(g)"> · {{ ioLive(g) }} live</template>
              </SoftBadge>
            </span>
          </button>
          <router-link :to="`/monitor/${g.loc.id}`" class="btn btn-xs btn-ghost shrink-0">
            Live View <span class="hidden sm:inline">→</span>
          </router-link>
        </div>

        <!-- Expanded body: the location's points, subsection by subsection. -->
        <div v-if="isExpanded(g.loc.id)" class="border-t border-base-300 divide-y divide-base-200">
          <div v-if="g.portals.length" class="p-4">
            <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2">🚪 Portals</div>
            <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
              <router-link
                v-for="p in g.portals"
                :key="p.id"
                :to="`/portals/${p.id}`"
                class="rounded-lg border border-base-300 bg-base-100 p-3 flex items-center justify-between gap-2 hover:border-primary/40 transition-colors min-w-0"
              >
                <span class="min-w-0">
                  <span class="font-medium text-sm truncate block">{{ p.name || p.code }}</span>
                  <code class="text-xs text-primary">{{ p.code }}</code>
                </span>
                <span class="flex items-center gap-1 shrink-0">
                  <SoftBadge v-if="statusByCode.get(p.code)?.held" tone="warning" dot>Held</SoftBadge>
                  <SoftBadge :tone="portalBadge(p).tone" dot>{{ portalBadge(p).text }}</SoftBadge>
                </span>
              </router-link>
            </div>
          </div>

          <div v-if="g.areas.length" class="p-4">
            <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2">🛡️ Areas</div>
            <AreaGrid :areas="g.areas" :shadows="areaShadowsByCode" flush />
          </div>

          <div v-if="g.auxInputs.length || g.auxOutputs.length" class="p-4">
            <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2">🔌 Aux I/O</div>
            <IoGrid :aux-inputs="g.auxInputs" :aux-outputs="g.auxOutputs" :status-by-key="auxStatusByKey" flush />
          </div>

          <div v-if="!g.portals.length && !g.areas.length && !g.auxInputs.length && !g.auxOutputs.length" class="p-4 text-sm opacity-50">
            No portals, areas, or aux I/O in this location.
          </div>
        </div>
      </div>

      <p v-if="!groups.length" class="text-sm opacity-50 text-center py-8">No locations yet.</p>
    </div>
  </div>
</template>
