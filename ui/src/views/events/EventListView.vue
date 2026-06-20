<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePagination } from '@/composables/usePagination'
import { formatDate, formatConstant } from '@/utils/format'
import type { AccessEvent, EventKind } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const { items: events, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessEvent>('events', 25)

const kindFilter = ref<EventKind | ''>('')
const searchQuery = ref('')
const selected = ref<AccessEvent | null>(null)

const KINDS: EventKind[] = ['tap', 'state', 'alarm', 'fire', 'command']

function queryOpts() {
  const filter = kindFilter.value ? `kind = "${kindFilter.value}"` : ''
  return { sort: '-ts,-created', filter }
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
  { key: 'location', label: 'Location' },
  { key: 'portal', label: 'Portal', mobileLabel: 'Portal' },
  { key: 'reason', label: 'Reason', format: (v) => (v ? formatConstant(v) : '-') },
]

function kindBadge(e: AccessEvent): string {
  if (e.kind === 'tap') return e.allow ? 'badge-success' : 'badge-error'
  if (e.kind === 'fire' || e.kind === 'alarm') return 'badge-warning'
  return 'badge-ghost'
}

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
    :has-query="!!searchQuery || !!kindFilter"
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
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(e) => selected = e">
        <template #cell-kind="{ item }"><span class="badge badge-sm" :class="kindBadge(item)">{{ item.kind || '—' }}</span></template>
        <template #card-kind="{ item }"><span class="badge badge-sm" :class="kindBadge(item)">{{ item.kind || '—' }}</span></template>
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

  <!-- Detail modal -->
    <Teleport to="body">
      <dialog class="modal" :class="{ 'modal-open': !!selected }">
        <div class="modal-box max-w-2xl" v-if="selected">
          <div class="flex justify-between items-center mb-4">
            <h3 class="font-bold text-lg flex items-center gap-2">
              <span class="badge" :class="kindBadge(selected)">{{ selected.kind }}</span>
              Event Detail
            </h3>
            <button @click="selected = null" class="btn btn-sm btn-circle btn-ghost">✕</button>
          </div>

          <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm mb-4">
            <div><span class="opacity-50 text-xs uppercase block">Time</span>{{ formatDate(selected.ts || selected.created, 'PPpp') }}</div>
            <div><span class="opacity-50 text-xs uppercase block">Location</span><code>{{ selected.location || '-' }}</code></div>
            <div><span class="opacity-50 text-xs uppercase block">Portal</span><code>{{ selected.portal || '-' }}</code></div>
            <div><span class="opacity-50 text-xs uppercase block">Allow</span>
              <span v-if="selected.kind === 'tap'" class="badge badge-sm" :class="selected.allow ? 'badge-success' : 'badge-error'">{{ selected.allow ? 'allow' : 'deny' }}</span>
              <span v-else class="opacity-40">n/a</span>
            </div>
            <div><span class="opacity-50 text-xs uppercase block">Credential</span><code>{{ selected.credential || '-' }}</code></div>
            <div><span class="opacity-50 text-xs uppercase block">User</span><code>{{ selected.user || '-' }}</code></div>
            <div class="col-span-2"><span class="opacity-50 text-xs uppercase block">Reason</span>{{ selected.reason ? formatConstant(selected.reason) : '-' }}</div>
          </div>

          <div class="bg-base-200 rounded-box overflow-hidden">
            <div class="px-3 py-2 text-xs font-medium opacity-60 border-b border-base-300">Payload</div>
            <pre class="p-3 text-xs overflow-x-auto"><code>{{ JSON.stringify(selected.payload ?? {}, null, 2) }}</code></pre>
          </div>

          <div class="modal-action">
            <button class="btn" @click="selected = null">Close</button>
          </div>
        </div>
        <div class="modal-backdrop" @click="selected = null"></div>
      </dialog>
    </Teleport>
</template>
