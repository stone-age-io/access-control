<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { formatRelativeTime, formatConstant } from '@/utils/format'
import { eventKindTone, unackedAlarmFilter } from '@/utils/events'
import { aggregateArm } from '@/utils/arming'
import type { AccessEvent, PointStatus } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'
import EventDetailModal from '@/components/ui/EventDetailModal.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

interface StatusCard {
  label: string
  icon: string
  path: string
  count: number | null
  /** Optional denominator, rendered as "count / total" (e.g. areas armed). */
  total?: number | null
  /** Informational, not a problem: skip the red "needs attention" styling. */
  neutral?: boolean
  /** Copy shown when the count is zero — the "nothing to worry about" state. */
  okHint: string
}

// Operational health, not inventory. The two questions an operator opens the app
// to answer — "is anything alarming?" and "is any edge box down?" — each linking
// to the page that resolves it. (Collection counts duplicated the sidebar and
// weren't actionable, so they're gone.)
const status = ref<StatusCard[]>([
  { label: 'Alarms to acknowledge', icon: '🚨', path: '/alarms', count: null, okHint: 'All clear' },
  { label: 'Controllers not online', icon: '⚙️', path: '/controllers', count: null, okHint: 'All online' },
  // Informational, not an alarm — armed is a normal state — so it stays neutral.
  { label: 'Areas armed', icon: '🛡️', path: '/areas', count: null, total: null, neutral: true, okHint: '' },
])

const recentEvents = ref<AccessEvent[]>([])
const selected = ref<AccessEvent | null>(null)
const loading = ref(true)

async function loadStatus() {
  const [alarmRes, ctrlRes, areaCountRes, shadowRes] = await Promise.allSettled([
    // Shared filter (utils/events) so this headline number matches the console.
    pb.collection('events').getList(1, 1, { filter: unackedAlarmFilter() }),
    // Anything not confirmed online — swept-offline *and* never-reported (empty
    // status, e.g. a new/undeployed box). A box we've never heard from is exactly
    // what an operator needs surfaced, so it counts here rather than hiding.
    pb.collection('controllers').getList(1, 1, { filter: 'status != "online"' }),
    // Areas armed: total is the denominator; the live count aggregates the per-
    // controller arm shadows the same way the Areas list and detail page do.
    pb.collection('areas').getList(1, 1),
    pb.collection('point_status').getFullList<PointStatus>({ filter: 'kind = "area"' }),
  ])
  status.value[0].count = alarmRes.status === 'fulfilled' ? alarmRes.value.totalItems : 0
  status.value[1].count = ctrlRes.status === 'fulfilled' ? ctrlRes.value.totalItems : 0
  status.value[2].count = armedAreaCount(shadowRes.status === 'fulfilled' ? shadowRes.value : [])
  status.value[2].total = areaCountRes.status === 'fulfilled' ? areaCountRes.value.totalItems : null
}

// armedAreaCount groups the kind=area shadows by area code and counts the areas whose
// aggregated state is fully armed (partial/unknown do not count as armed).
function armedAreaCount(rows: PointStatus[]): number {
  const byCode = new Map<string, PointStatus[]>()
  for (const r of rows) byCode.set(r.code, [...(byCode.get(r.code) ?? []), r])
  let n = 0
  for (const group of byCode.values()) if (aggregateArm(group).state === 'armed') n++
  return n
}

async function loadRecentEvents() {
  try {
    const res = await pb.collection('events').getList<AccessEvent>(1, 8, { sort: '-ts,-created' })
    recentEvents.value = res.items
  } catch {
    recentEvents.value = []
  }
}

onMounted(async () => {
  loading.value = true
  await Promise.all([loadStatus(), loadRecentEvents()])
  loading.value = false
})
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-bold">Overview</h1>
      <p class="text-base-content/70 mt-1">System health and recent access activity.</p>
    </div>

    <!-- Operational status — turns red when something needs attention (neutral cards
         stay calm: an armed area is a normal state, not a problem). -->
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
      <router-link
        v-for="s in status"
        :key="s.path"
        :to="s.path"
        class="card border shadow-sm transition-all hover:shadow-md"
        :class="!s.neutral && (s.count ?? 0) > 0
          ? 'bg-error/5 border-error/40 hover:border-error/60'
          : 'bg-base-100 border-base-300 hover:border-primary/40'"
      >
        <div class="card-body p-4">
          <div class="flex items-center justify-between">
            <span class="text-2xl">{{ s.icon }}</span>
            <span
              class="text-3xl font-bold tabular-nums"
              :class="!s.neutral && (s.count ?? 0) > 0 ? 'text-error' : 'opacity-80'"
            >
              <span v-if="s.count === null" class="loading loading-dots loading-sm opacity-40"></span>
              <template v-else>
                {{ s.count }}<span v-if="s.total != null" class="text-lg opacity-50"> / {{ s.total }}</span>
              </template>
            </span>
          </div>
          <div class="text-sm font-medium opacity-70 mt-1">{{ s.label }}</div>
          <div v-if="s.count === 0 && s.okHint" class="text-xs text-success/80 mt-0.5">{{ s.okHint }}</div>
        </div>
      </router-link>
    </div>

    <!-- Recent activity -->
    <BaseCard title="Recent Activity">
      <template #actions>
        <router-link to="/events" class="btn btn-ghost btn-sm">View all</router-link>
      </template>

      <div v-if="loading" class="flex justify-center p-6">
        <span class="loading loading-spinner loading-md opacity-40"></span>
      </div>

      <div v-else-if="recentEvents.length === 0" class="text-center py-8 opacity-50">
        <span class="text-3xl">🛈</span>
        <p class="text-sm mt-2">No events yet. They appear here once controllers start publishing to ACC_EVENTS.</p>
      </div>

      <ul v-else class="divide-y divide-base-200">
        <li
          v-for="e in recentEvents"
          :key="e.id"
          class="flex items-center gap-3 py-2.5 -mx-2 px-2 rounded-lg cursor-pointer hover:bg-base-200/60 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary/60"
          role="button"
          tabindex="0"
          @click="selected = e"
          @keydown.enter.prevent="selected = e"
          @keydown.space.prevent="selected = e"
        >
          <SoftBadge :tone="eventKindTone(e)" dot>{{ e.kind || 'event' }}</SoftBadge>
          <div class="flex-1 min-w-0">
            <div class="text-sm truncate">
              <span class="font-medium">{{ e.location || '—' }}</span>
              <span v-if="e.portal" class="opacity-60"> / {{ e.portal }}</span>
              <span v-if="e.reason" class="opacity-60"> — {{ formatConstant(e.reason) }}</span>
            </div>
            <div v-if="e.credential || e.user" class="text-xs opacity-50 truncate">
              <span v-if="e.credential" class="font-mono">{{ e.credential }}</span>
              <span v-if="e.user"> · {{ e.user }}</span>
            </div>
          </div>
          <span class="text-xs opacity-50 whitespace-nowrap">{{ formatRelativeTime(e.ts || e.created) }}</span>
        </li>
      </ul>
    </BaseCard>

    <EventDetailModal :event="selected" @close="selected = null" />
  </div>
</template>
