<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePagination } from '@/composables/usePagination'
import { formatDate, formatConstant } from '@/utils/format'
import type { AccessEvent, EventKind } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

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
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-bold">Events</h1>
      <p class="text-base-content/70 mt-1">Audit timeline — the queryable projection of the ACC_EVENTS stream.</p>
    </div>

    <div class="flex flex-col sm:flex-row gap-3">
      <select v-model="kindFilter" class="select select-bordered sm:w-48" @change="loadEvents">
        <option value="">All kinds</option>
        <option v-for="k in KINDS" :key="k" :value="k">{{ formatConstant(k) }}</option>
      </select>
      <input v-model="searchQuery" type="text" placeholder="Filter this page by location, portal, credential, user, reason..." class="input input-bordered flex-1" />
    </div>

    <div v-if="loading && events.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && events.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load events</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="loadEvents" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="events.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">📋</span>
        <h3 class="text-xl font-bold mt-4">No events</h3>
        <p class="text-base-content/70 mt-2">Events appear once controllers publish taps, state changes, and alarms.</p>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(e) => selected = e">
        <template #cell-kind="{ item }"><span class="badge badge-sm" :class="kindBadge(item)">{{ item.kind || '—' }}</span></template>
        <template #card-kind="{ item }"><span class="badge badge-sm" :class="kindBadge(item)">{{ item.kind || '—' }}</span></template>
        <template #cell-location="{ item }"><code class="text-xs">{{ item.location || '-' }}</code></template>

        <template #actions="{ item }">
          <button class="btn btn-xs" @click="selected = item">Details</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/70">{{ events.length }} of {{ totalItems }} events</span>
        <div class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>

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
  </div>
</template>
