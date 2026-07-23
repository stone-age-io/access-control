<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePagination } from '@/composables/usePagination'
import { pb } from '@/utils/pb'
import { formatDate, formatConstant, localInputToISO } from '@/utils/format'
import { eventKindTone, tsRangeClauses } from '@/utils/events'
import { toCsv, downloadCsv, fileStamp, type CsvColumn } from '@/utils/csv'
import type { AccessEvent, EventKind, EventSource } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import EventDetailModal from '@/components/ui/EventDetailModal.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const { items: events, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessEvent>('events', 25)

const kindFilter = ref<EventKind | ''>('')
const sourceFilter = ref<EventSource | ''>('')
const fromFilter = ref('')
const toFilter = ref('')
const searchQuery = ref('')
const selected = ref<AccessEvent | null>(null)

const KINDS: EventKind[] = ['tap', 'state', 'alarm', 'fire', 'command']
const SOURCES: EventSource[] = ['nats', 'osdp']

function queryOpts() {
  const clauses: string[] = []
  if (kindFilter.value) clauses.push(`kind = "${kindFilter.value}"`)
  if (sourceFilter.value) clauses.push(`source = "${sourceFilter.value}"`)
  clauses.push(...tsRangeClauses(localInputToISO(fromFilter.value), localInputToISO(toFilter.value)))
  return { sort: '-ts,-created', filter: clauses.join(' && ') }
}

function loadEvents() {
  page.value = 1
  load(queryOpts())
}

function clearRange() {
  fromFilter.value = ''
  toFilter.value = ''
  loadEvents()
}

// CSV export of the *filtered* set (every match, not just this page). getFullList
// batches under the hood; the active time range is the natural bound on size.
const exporting = ref(false)
const exportError = ref('')

interface EventRow {
  ts: string
  kind: string
  source: string
  location: string
  portal: string
  type: string
  credential: string
  user: string
  result: string
  reason: string
}

const EXPORT_COLUMNS: CsvColumn<EventRow>[] = [
  { key: 'ts', label: 'Time (UTC)' },
  { key: 'kind', label: 'Kind' },
  { key: 'source', label: 'Source' },
  { key: 'location', label: 'Location' },
  { key: 'portal', label: 'Portal' },
  { key: 'type', label: 'Type' },
  { key: 'credential', label: 'Credential' },
  { key: 'user', label: 'User' },
  { key: 'result', label: 'Result' },
  { key: 'reason', label: 'Reason' },
]

async function exportCsv() {
  exporting.value = true
  exportError.value = ''
  try {
    const opts = queryOpts()
    const rows = await pb.collection('events').getFullList<AccessEvent>({
      filter: opts.filter || undefined,
      sort: opts.sort,
      batch: 500,
    })
    const mapped: EventRow[] = rows.map((e) => ({
      ts: e.ts || e.created,
      kind: e.kind,
      source: e.source,
      location: e.location,
      portal: e.portal,
      type: e.type,
      credential: e.credential,
      user: e.user,
      result: e.kind === 'tap' ? (e.allow ? 'allow' : 'deny') : '',
      reason: e.reason,
    }))
    downloadCsv(`events-${fileStamp()}.csv`, toCsv(mapped, EXPORT_COLUMNS))
  } catch (err: any) {
    exportError.value = err?.message || 'Export failed'
  } finally {
    exporting.value = false
  }
}

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return events.value
  return events.value.filter(e =>
    e.location?.toLowerCase().includes(q) ||
    e.portal?.toLowerCase().includes(q) ||
    e.credential?.toLowerCase().includes(q) ||
    e.user?.toLowerCase().includes(q) ||
    e.reason?.toLowerCase().includes(q)
  )
})

const columns: Column<AccessEvent>[] = [
  { key: 'ts', label: 'Time', format: (v, item) => formatDate(v || item.created, 'PP p') },
  { key: 'kind', label: 'Kind' },
  { key: 'source', label: 'Source' },
  { key: 'location', label: 'Location' },
  { key: 'portal', label: 'Portal', mobileLabel: 'Portal' },
  { key: 'reason', label: 'Reason', format: (v) => (v ? formatConstant(v) : '-') },
]

onMounted(loadEvents)
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Events"
    subtitle="Audit timeline — the queryable projection of the ACC_EVENTS stream."
    search-placeholder="Filter this page by location, portal, credential, user, reason..."
    :loading="loading"
    :error="error"
    :is-empty="events.length === 0"
    :has-query="!!searchQuery || !!kindFilter || !!sourceFilter || !!fromFilter || !!toFilter"
    empty-icon="📋"
    empty-title="No events"
    empty-message="Events appear once controllers publish taps, state changes, and alarms."
    error-title="Failed to load events"
    @retry="loadEvents"
  >
    <template #actions>
      <button class="btn btn-sm" :disabled="exporting || events.length === 0" @click="exportCsv">
        <span v-if="exporting" class="loading loading-spinner loading-xs"></span>
        Export CSV
      </button>
    </template>

    <template #toolbar>
      <select v-model="kindFilter" class="select select-bordered sm:w-48" @change="loadEvents">
        <option value="">All kinds</option>
        <option v-for="k in KINDS" :key="k" :value="k">{{ formatConstant(k) }}</option>
      </select>
      <select v-model="sourceFilter" class="select select-bordered sm:w-40" @change="loadEvents">
        <option value="">All sources</option>
        <option v-for="s in SOURCES" :key="s" :value="s">{{ formatConstant(s) }}</option>
      </select>
      <label class="input input-bordered flex items-center gap-2 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">From</span>
        <input v-model="fromFilter" type="datetime-local" class="grow bg-transparent" @change="loadEvents" />
      </label>
      <label class="input input-bordered flex items-center gap-2 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">To</span>
        <input v-model="toFilter" type="datetime-local" class="grow bg-transparent" @change="loadEvents" />
      </label>
      <button v-if="fromFilter || toFilter" class="btn btn-ghost" @click="clearRange">Clear dates</button>
    </template>

    <div v-if="exportError" class="alert alert-error">
      <span>{{ exportError }}</span>
      <button class="btn btn-ghost btn-xs" @click="exportError = ''">Dismiss</button>
    </div>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(e) => selected = e">
        <template #cell-kind="{ item }"><SoftBadge :tone="eventKindTone(item)" dot>{{ item.kind || '—' }}</SoftBadge></template>
        <template #card-kind="{ item }"><SoftBadge :tone="eventKindTone(item)" dot>{{ item.kind || '—' }}</SoftBadge></template>
        <template #cell-source="{ item }"><SoftBadge v-if="item.source">{{ item.source }}</SoftBadge><span v-else class="opacity-40">—</span></template>
        <template #card-source="{ item }"><SoftBadge v-if="item.source">{{ item.source }}</SoftBadge><span v-else class="opacity-40">—</span></template>
        <template #cell-location="{ item }"><code class="text-xs">{{ item.location || '-' }}</code></template>

        <template #actions="{ item }">
          <button class="btn btn-xs" @click="selected = item">Details</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ events.length }} of {{ totalItems }} events
      </ListPagination>
    </BaseCard>
  </ListLayout>

  <EventDetailModal :event="selected" @close="selected = null" />
</template>
