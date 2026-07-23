<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { pb } from '@/utils/pb'
import { usePagination } from '@/composables/usePagination'
import { useAuthStore } from '@/stores/auth'
import { useAlarmAck } from '@/composables/useAlarmAck'
import { useConfirm } from '@/composables/useConfirm'
import { formatDate, formatRelativeTime, formatConstant } from '@/utils/format'
import { alarmType, alarmTone, alarmToneForType, alarmTypeClause, eventThing, unackedAlarmFilter } from '@/utils/events'
import type { AccessEvent, Location } from '@/types/pocketbase'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
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
// Per-type unacked totals across the whole window (see loadCounts) — drives the
// triage summary tiles, which double as the type filter.
const counts = ref<Record<string, number>>({})
let unsub: (() => void) | null = null

const canCommand = computed(() => auth.can('command'))

// Operator-facing alarm sub-types for the toolbar filter (held_clear is a clear,
// not something you triage, so it's omitted — leaving it unfiltered still shows it).
const ALARM_TYPES = ['forced', 'held', 'intrusion', 'tamper_24h', 'fire']

// Short tile/badge labels — formatConstant would render 'tamper_24h' as 'Tamper 24h'.
const TYPE_LABELS: Record<string, string> = {
  forced: 'Forced',
  held: 'Held',
  intrusion: 'Intrusion',
  tamper_24h: 'Tamper',
  fire: 'Fire',
}
// Severity tone → utility classes for the summary tiles and the row accent stripe.
const TONE_TEXT: Record<string, string> = { error: 'text-error', warning: 'text-warning', neutral: 'text-base-content/60' }
const TONE_DOT: Record<string, string> = { error: 'bg-error', warning: 'bg-warning', neutral: 'bg-base-content/40' }
const TONE_BORDER: Record<string, string> = { error: 'border-error', warning: 'border-warning', neutral: 'border-base-300' }

function typeLabel(t: string): string {
  return TYPE_LABELS[t] || formatConstant(t)
}
function accentBorder(e: AccessEvent): string {
  return TONE_BORDER[alarmTone(e)]
}

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
  loadCounts()
}

// Single reconcile path for both acks and live updates: reload the current page
// so counts/paging stay correct. If acks emptied the page past the end, snap back
// to the last real page so the operator isn't stranded on a blank page.
async function reconcile() {
  loadCounts()
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

// Per-type unacked totals across the whole window — not just the loaded page — so
// the tiles are a real triage count. Respects the location filter (so tiles track a
// location narrowing) but not the type filter (each tile owns its own type) nor the
// client-side page search. One cheap count query per type, run in parallel.
async function loadCounts() {
  const locClause = locationFilter.value ? [`location = "${locationFilter.value}"`] : []
  const entries = await Promise.all(
    ALARM_TYPES.map(async (t) => {
      try {
        const res = await pb.collection('events').getList(1, 1, {
          filter: unackedAlarmFilter([...alarmTypeClause(t), ...locClause]),
        })
        return [t, res.totalItems] as const
      } catch {
        return [t, 0] as const // fail-safe: a count of 0, console still works
      }
    }),
  )
  counts.value = Object.fromEntries(entries)
}

// Tiles double as the type filter: click to narrow, click the active one to clear.
function selectType(t: string) {
  typeFilter.value = typeFilter.value === t ? '' : t
  reload()
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
        <option v-for="t in ALARM_TYPES" :key="t" :value="t">{{ typeLabel(t) }}</option>
      </select>
      <select v-model="locationFilter" class="select select-bordered sm:w-48" @change="reload">
        <option value="">All locations</option>
        <option v-for="l in locations" :key="l.id" :value="l.code">{{ l.name || l.code }}</option>
      </select>
    </template>

    <!-- Triage summary: unacked totals per type across the whole window. Each tile
         is also the type filter — click to narrow, click the active one to clear. -->
    <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
      <button
        v-for="t in ALARM_TYPES"
        :key="t"
        type="button"
        class="rounded-lg border px-3 py-2 text-left transition-colors"
        :class="typeFilter === t
          ? 'border-primary bg-base-100 ring-1 ring-primary/30'
          : 'border-base-300 bg-base-200/40 hover:bg-base-200'"
        :aria-pressed="typeFilter === t"
        @click="selectType(t)"
      >
        <div
          class="flex items-center gap-1.5 text-xs font-medium"
          :class="(counts[t] || 0) > 0 ? TONE_TEXT[alarmToneForType(t)] : 'text-base-content/50'"
        >
          <span
            class="inline-block h-1.5 w-1.5 rounded-full"
            :class="(counts[t] || 0) > 0 ? TONE_DOT[alarmToneForType(t)] : 'bg-base-content/25'"
          ></span>
          {{ typeLabel(t) }}
        </div>
        <div
          class="mt-0.5 text-2xl font-bold tabular-nums"
          :class="(counts[t] || 0) > 0 ? TONE_TEXT[alarmToneForType(t)] : 'text-base-content/40'"
        >
          {{ counts[t] || 0 }}
        </div>
      </button>
    </div>

    <BaseCard :no-padding="true">
      <ul v-if="filtered.length" class="divide-y divide-base-200">
        <li
          v-for="e in filtered"
          :key="e.id"
          class="flex items-center justify-between gap-3 border-l-4 py-3 pl-3 pr-4 cursor-pointer transition-colors hover:bg-base-200/60 focus-visible:outline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary/60"
          :class="accentBorder(e)"
          role="button"
          tabindex="0"
          :aria-label="`View ${typeLabel(alarmType(e))} alarm detail`"
          @click="selected = e"
          @keydown.enter.prevent="selected = e"
          @keydown.space.prevent="selected = e"
        >
          <div class="flex items-center gap-3 min-w-0">
            <SoftBadge :tone="alarmTone(e)" dot class="shrink-0">{{ typeLabel(alarmType(e)) }}</SoftBadge>
            <div class="min-w-0">
              <div class="truncate">
                <code class="text-sm">{{ eventThing(e) }}</code>
                <SoftBadge v-if="e.location" class="ml-2 align-middle">{{ e.location }}</SoftBadge>
              </div>
              <div class="text-xs text-base-content/50" :title="formatDate(e.ts || e.created, 'PPpp')">
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
