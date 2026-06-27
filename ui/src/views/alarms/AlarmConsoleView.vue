<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { pb } from '@/utils/pb'
import { usePagination } from '@/composables/usePagination'
import { useAuthStore } from '@/stores/auth'
import { useAlarmAck } from '@/composables/useAlarmAck'
import { useConfirm } from '@/composables/useConfirm'
import { formatDate, formatRelativeTime, formatConstant } from '@/utils/format'
import { alarmType, alarmTypeBadge, alarmTypeClause, eventThing, unackedAlarmFilter } from '@/utils/events'
import type { AccessEvent, Location } from '@/types/pocketbase'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import EventDetailModal from '@/components/ui/EventDetailModal.vue'

const auth = useAuthStore()
const { acking, ack, ackMany } = useAlarmAck()
const { confirm } = useConfirm()

// usePagination owns the paged load; `alarms` is just its `items`. This replaces
// the old getList(1, 200) hard cap that silently hid alarms past 200.
const { items: alarms, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessEvent>('events', 25)

const selected = ref<AccessEvent | null>(null)
const searchQuery = ref('')
const typeFilter = ref('')
const locationFilter = ref('')
const locations = ref<Location[]>([])
let unsub: (() => void) | null = null

const canCommand = computed(() => auth.can('command'))

// Operator-facing alarm sub-types for the toolbar filter (held_clear is a clear,
// not something you triage, so it's omitted — leaving it unfiltered still shows it).
const ALARM_TYPES = ['forced', 'held', 'intrusion', 'tamper_24h', 'fire']

const hasQuery = computed(() => !!searchQuery.value || !!typeFilter.value || !!locationFilter.value)

function isAlarm(e: AccessEvent): boolean {
  return e.kind === 'alarm' || e.kind === 'fire'
}

function queryOpts() {
  const extra: string[] = [...alarmTypeClause(typeFilter.value)]
  if (locationFilter.value) extra.push(`location = "${locationFilter.value}"`)
  return { filter: unackedAlarmFilter(extra), sort: '-created' }
}

// Client-side narrowing of the loaded page (mirrors EventListView's this-page
// search). Server filters do the cross-page work; this just refines what's shown.
const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return alarms.value
  return alarms.value.filter((e) =>
    e.location?.toLowerCase().includes(q) ||
    e.portal?.toLowerCase().includes(q) ||
    eventThing(e).toLowerCase().includes(q) ||
    alarmType(e).toLowerCase().includes(q),
  )
})

function reload() {
  page.value = 1
  load(queryOpts())
}

// Single reconcile path for both acks and live updates: reload the current page
// so counts/paging stay correct. If acks emptied the page past the end, snap back
// to the last real page so the operator isn't stranded on a blank page.
async function reconcile() {
  await load(queryOpts())
  if (alarms.value.length === 0 && page.value > 1 && page.value > totalPages.value) {
    page.value = Math.max(1, totalPages.value)
    await load(queryOpts())
  }
}

// Coalesce bursts (a flood of alarms, or an ack fanning out updates) into one
// reload, so the live console stays responsive without thrashing the server.
let reconcileTimer: ReturnType<typeof setTimeout> | null = null
function scheduleReconcile() {
  if (reconcileTimer) clearTimeout(reconcileTimer)
  reconcileTimer = setTimeout(() => { reconcile() }, 400)
}

async function loadLocations() {
  try {
    locations.value = await pb.collection('locations').getFullList<Location>({ sort: 'code' })
  } catch {
    // Fail-safe: no location filter, but the console still works.
    locations.value = []
  }
}

async function subscribe() {
  unsub = await pb.collection('events').subscribe<AccessEvent>('*', (e) => {
    const rec = e.record
    // A new unacked alarm, or one acked/deleted elsewhere, changes the set —
    // reconcile the current page (debounced) so counts and paging stay correct.
    if (e.action === 'create' && isAlarm(rec) && !rec.acknowledged) scheduleReconcile()
    else if (e.action === 'update' && isAlarm(rec)) scheduleReconcile()
    else if (e.action === 'delete' && isAlarm(rec)) scheduleReconcile()
  })
}

async function acknowledge(e: AccessEvent) {
  if (await ack(e.id)) {
    alarms.value = alarms.value.filter((a) => a.id !== e.id) // optimistic
    if (selected.value?.id === e.id) selected.value = null
    scheduleReconcile()
  }
}

async function ackAll() {
  // Ack what the operator sees — the filtered page, not hidden rows.
  const ids = filtered.value.map((a) => a.id)
  if (ids.length === 0) return
  const yes = await confirm({
    title: 'Acknowledge alarms',
    message: `Acknowledge ${ids.length} alarm${ids.length === 1 ? '' : 's'} on this page?`,
    confirmText: 'Acknowledge all',
    variant: 'warning',
  })
  if (!yes) return
  const ok = await ackMany(ids)
  if (ok === ids.length) {
    const acked = new Set(ids)
    alarms.value = alarms.value.filter((a) => !acked.has(a.id)) // optimistic on full success
  }
  scheduleReconcile()
}

onMounted(() => {
  reload()
  loadLocations()
  subscribe()
})
onBeforeUnmount(() => {
  if (unsub) unsub()
  if (reconcileTimer) clearTimeout(reconcileTimer)
})
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Alarm Console"
    subtitle="Unacknowledged alarms — forced/held doors, intrusion trips, and fire input (last 7 days)."
    search-placeholder="Filter this page by portal, point, location..."
    :loading="loading"
    :error="error"
    :is-empty="alarms.length === 0"
    :has-query="hasQuery"
    empty-icon="✅"
    empty-title="All clear"
    empty-message="No unacknowledged alarms."
    error-title="Failed to load alarms"
    @retry="reload"
  >
    <template #actions>
      <button
        v-if="canCommand"
        class="btn btn-sm"
        :disabled="acking || filtered.length === 0"
        @click="ackAll"
      >
        <span v-if="acking" class="loading loading-spinner loading-xs"></span>
        Ack all on page
      </button>
      <button class="btn btn-ghost btn-sm" :disabled="loading" @click="reload">Refresh</button>
    </template>

    <template #toolbar>
      <select v-model="typeFilter" class="select select-bordered sm:w-48" @change="reload">
        <option value="">All types</option>
        <option v-for="t in ALARM_TYPES" :key="t" :value="t">{{ formatConstant(t) }}</option>
      </select>
      <select v-model="locationFilter" class="select select-bordered sm:w-48" @change="reload">
        <option value="">All locations</option>
        <option v-for="l in locations" :key="l.id" :value="l.code">{{ l.name || l.code }}</option>
      </select>
    </template>

    <BaseCard :no-padding="true">
      <ul v-if="filtered.length" class="divide-y divide-base-200">
        <li
          v-for="e in filtered"
          :key="e.id"
          class="flex items-center justify-between gap-3 p-4 cursor-pointer transition-colors hover:bg-base-200/60 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary/60"
          role="button"
          tabindex="0"
          :aria-label="`View ${formatConstant(alarmType(e))} alarm detail`"
          @click="selected = e"
          @keydown.enter.prevent="selected = e"
          @keydown.space.prevent="selected = e"
        >
          <div class="flex items-center gap-3 min-w-0">
            <span class="badge" :class="alarmTypeBadge(e)">{{ formatConstant(alarmType(e)) }}</span>
            <div class="min-w-0">
              <div class="font-medium truncate">
                <code class="text-sm">{{ eventThing(e) }}</code>
                <span class="opacity-50 text-xs ml-2">{{ e.location }}</span>
              </div>
              <div class="text-xs opacity-50" :title="formatDate(e.ts || e.created, 'PPpp')">
                {{ formatRelativeTime(e.ts || e.created) }}
              </div>
            </div>
          </div>
          <button
            class="btn btn-sm btn-primary shrink-0"
            :disabled="acking || !canCommand"
            :title="canCommand ? 'Acknowledge' : 'Requires the command capability'"
            @click.stop="acknowledge(e)"
          >
            Ack
          </button>
        </li>
      </ul>
      <div v-else class="p-8 text-center text-sm text-base-content/60">
        No alarms match these filters.
      </div>

      <ListPagination
        :page="page"
        :total-pages="totalPages"
        :loading="loading"
        @prev="prevPage(queryOpts())"
        @next="nextPage(queryOpts())"
      >
        {{ alarms.length }} of {{ totalItems }} unacknowledged
      </ListPagination>
    </BaseCard>
  </ListLayout>

  <EventDetailModal :event="selected" @close="selected = null" />
</template>
