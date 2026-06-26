<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { formatDate, formatConstant, localInputToISO, isoToLocalInput } from '@/utils/format'
import { tsRangeClauses } from '@/utils/events'
import { toCsv, downloadCsv, fileStamp, type CsvColumn } from '@/utils/csv'
import type { AccessEvent } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

// Denied access = a credential presentation (tap) the policy rejected. The deny
// reason codes are a stable contract, so a breakdown by reason answers "why are
// people being turned away" — misconfigured access, expired badges, after-hours,
// or probing. We fetch the whole range once and derive every view client-side;
// the date range (default: last 7 days) is the natural bound on size.
const DEFAULT_DAYS = 7
const DISPLAY_CAP = 500

const fromFilter = ref(isoToLocalInput(new Date(Date.now() - DEFAULT_DAYS * 86400000)))
const toFilter = ref('')
const search = ref('')

const denials = ref<AccessEvent[]>([])
const loading = ref(false)
const error = ref('')

function rangeFilter(): string {
  const clauses = ['kind = "tap"', 'allow = false']
  clauses.push(...tsRangeClauses(localInputToISO(fromFilter.value), localInputToISO(toFilter.value)))
  return clauses.join(' && ')
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    denials.value = await pb.collection('events').getFullList<AccessEvent>({
      filter: rangeFilter(),
      sort: '-ts,-created',
      batch: 500,
    })
  } catch (err: any) {
    error.value = err?.message || 'Failed to load denials'
    denials.value = []
  } finally {
    loading.value = false
  }
}

function clearRange() {
  fromFilter.value = ''
  toFilter.value = ''
  load()
}

// Client-side text filter over the loaded set (the range is the server-side cut).
const filtered = computed(() => {
  const q = search.value.toLowerCase().trim()
  if (!q) return denials.value
  return denials.value.filter((e) =>
    e.location?.toLowerCase().includes(q) ||
    e.portal?.toLowerCase().includes(q) ||
    e.credential?.toLowerCase().includes(q) ||
    e.user?.toLowerCase().includes(q) ||
    e.reason?.toLowerCase().includes(q),
  )
})

const display = computed(() => filtered.value.slice(0, DISPLAY_CAP))
const truncated = computed(() => filtered.value.length > DISPLAY_CAP)

interface Tally { key: string; count: number }
function tally(by: (e: AccessEvent) => string): Tally[] {
  const m = new Map<string, number>()
  for (const e of filtered.value) {
    const k = by(e) || '—'
    m.set(k, (m.get(k) || 0) + 1)
  }
  return [...m.entries()].map(([key, count]) => ({ key, count })).sort((a, b) => b.count - a.count)
}

const byReason = computed(() => tally((e) => e.reason))
const byPortal = computed(() => tally((e) => e.portal || e.location))

interface DenialRow {
  ts: string
  location: string
  portal: string
  credential: string
  cardholder: string
  reason: string
}

const EXPORT_COLUMNS: CsvColumn<DenialRow>[] = [
  { key: 'ts', label: 'Time (UTC)' },
  { key: 'location', label: 'Location' },
  { key: 'portal', label: 'Portal' },
  { key: 'credential', label: 'Credential' },
  { key: 'cardholder', label: 'Cardholder' },
  { key: 'reason', label: 'Reason' },
]

function exportCsv() {
  const rows: DenialRow[] = filtered.value.map((e) => ({
    ts: e.ts || e.created,
    location: e.location,
    portal: e.portal,
    credential: e.credential,
    cardholder: e.user,
    reason: e.reason,
  }))
  downloadCsv(`denied-access-${fileStamp()}.csv`, toCsv(rows, EXPORT_COLUMNS))
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Filters -->
    <div class="flex flex-col sm:flex-row sm:flex-wrap gap-3">
      <label class="input input-bordered flex items-center gap-2 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">From</span>
        <input v-model="fromFilter" type="datetime-local" class="grow bg-transparent" @change="load" />
      </label>
      <label class="input input-bordered flex items-center gap-2 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">To</span>
        <input v-model="toFilter" type="datetime-local" class="grow bg-transparent" @change="load" />
      </label>
      <button v-if="fromFilter || toFilter" class="btn btn-ghost" @click="clearRange">Clear dates</button>
      <label class="input input-bordered flex items-center gap-2 flex-1 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">🔎</span>
        <input v-model="search" type="text" class="grow" placeholder="Filter by location, portal, credential, cardholder, reason..." />
      </label>
      <button class="btn" :disabled="loading || filtered.length === 0" @click="exportCsv">Export CSV</button>
    </div>

    <div v-if="error" class="alert alert-error">
      <span>{{ error }}</span>
      <button class="btn btn-ghost btn-xs" @click="load">Retry</button>
    </div>

    <div v-if="loading" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <template v-else>
      <!-- Summary -->
      <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <BaseCard>
          <div class="text-sm opacity-60">Denials in range</div>
          <div class="text-3xl font-bold tabular-nums mt-1">{{ filtered.length }}</div>
          <div class="text-xs opacity-50 mt-1">tap events the policy rejected</div>
        </BaseCard>

        <BaseCard title="Top reasons">
          <ul v-if="byReason.length" class="space-y-1">
            <li v-for="r in byReason.slice(0, 6)" :key="r.key" class="flex items-center justify-between gap-2 text-sm">
              <span class="truncate">{{ formatConstant(r.key) }}</span>
              <span class="badge badge-sm badge-error tabular-nums">{{ r.count }}</span>
            </li>
          </ul>
          <div v-else class="text-sm opacity-50">No denials in range.</div>
        </BaseCard>

        <BaseCard title="Top portals">
          <ul v-if="byPortal.length" class="space-y-1">
            <li v-for="p in byPortal.slice(0, 6)" :key="p.key" class="flex items-center justify-between gap-2 text-sm">
              <span class="truncate font-mono text-xs">{{ p.key }}</span>
              <span class="badge badge-sm tabular-nums">{{ p.count }}</span>
            </li>
          </ul>
          <div v-else class="text-sm opacity-50">No denials in range.</div>
        </BaseCard>
      </div>

      <!-- Detail list -->
      <BaseCard :no-padding="true">
        <div v-if="filtered.length === 0" class="text-center py-12 opacity-50">
          <span class="text-3xl">✅</span>
          <p class="text-sm mt-2">No denied presentations in this range.</p>
        </div>
        <div v-else class="overflow-x-auto">
          <table class="table table-sm">
            <thead>
              <tr>
                <th>Time</th>
                <th>Location</th>
                <th>Portal</th>
                <th>Credential</th>
                <th>Cardholder</th>
                <th>Reason</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in display" :key="e.id">
                <td class="whitespace-nowrap">{{ formatDate(e.ts || e.created, 'PP p') }}</td>
                <td><code class="text-xs">{{ e.location || '-' }}</code></td>
                <td>{{ e.portal || '-' }}</td>
                <td class="font-mono text-xs">{{ e.credential || '-' }}</td>
                <td>{{ e.user || '-' }}</td>
                <td><span class="badge badge-sm badge-error">{{ formatConstant(e.reason) || '-' }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="truncated" class="p-3 text-xs opacity-60 border-t border-base-200">
          Showing the first {{ DISPLAY_CAP }} of {{ filtered.length }} denials. Narrow the range, or use Export CSV for the full set.
        </div>
      </BaseCard>
    </template>
  </div>
</template>
