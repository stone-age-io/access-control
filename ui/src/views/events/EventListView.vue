<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePagination } from '@/composables/usePagination'
import { formatDate, formatConstant } from '@/utils/format'
import { eventKindBadge } from '@/utils/events'
import type { AccessEvent, EventKind, EventSource } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import EventDetailModal from '@/components/ui/EventDetailModal.vue'

const { items: events, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessEvent>('events', 25)

const kindFilter = ref<EventKind | ''>('')
const sourceFilter = ref<EventSource | ''>('')
const searchQuery = ref('')
const selected = ref<AccessEvent | null>(null)

const KINDS: EventKind[] = ['tap', 'state', 'alarm', 'fire', 'command']
const SOURCES: EventSource[] = ['nats', 'osdp']

function queryOpts() {
  const clauses: string[] = []
  if (kindFilter.value) clauses.push(`kind = "${kindFilter.value}"`)
  if (sourceFilter.value) clauses.push(`source = "${sourceFilter.value}"`)
  return { sort: '-ts,-created', filter: clauses.join(' && ') }
}

function loadEvents() {
  page.value = 1
  load(queryOpts())
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
    :has-query="!!searchQuery || !!kindFilter || !!sourceFilter"
    empty-icon="📋"
    empty-title="No events"
    empty-message="Events appear once controllers publish taps, state changes, and alarms."
    error-title="Failed to load events"
    @retry="loadEvents"
  >
    <template #toolbar>
      <select v-model="kindFilter" class="select select-bordered sm:w-48" @change="loadEvents">
        <option value="">All kinds</option>
        <option v-for="k in KINDS" :key="k" :value="k">{{ formatConstant(k) }}</option>
      </select>
      <select v-model="sourceFilter" class="select select-bordered sm:w-40" @change="loadEvents">
        <option value="">All sources</option>
        <option v-for="s in SOURCES" :key="s" :value="s">{{ formatConstant(s) }}</option>
      </select>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(e) => selected = e">
        <template #cell-kind="{ item }"><span class="badge badge-sm" :class="eventKindBadge(item)">{{ item.kind || '—' }}</span></template>
        <template #card-kind="{ item }"><span class="badge badge-sm" :class="eventKindBadge(item)">{{ item.kind || '—' }}</span></template>
        <template #cell-source="{ item }"><span v-if="item.source" class="badge badge-sm badge-ghost">{{ item.source }}</span><span v-else class="opacity-40">—</span></template>
        <template #card-source="{ item }"><span v-if="item.source" class="badge badge-sm badge-ghost">{{ item.source }}</span><span v-else class="opacity-40">—</span></template>
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
