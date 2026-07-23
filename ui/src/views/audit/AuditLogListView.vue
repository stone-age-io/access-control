<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { usePagination } from '@/composables/usePagination'
import { formatDate, formatConstant } from '@/utils/format'
import type { AuditLog, AuditEventType } from '@/types/pocketbase'
import type { SoftTone } from '@/utils/badges'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const { items: logs, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AuditLog>('audit_logs', 25)

const typeFilter = ref<AuditEventType | ''>('')
const searchQuery = ref('')
const selected = ref<AuditLog | null>(null)

const TYPES: AuditEventType[] = ['create', 'update', 'delete', 'auth']

function queryOpts() {
  const filter = typeFilter.value ? `event_type = "${typeFilter.value}"` : ''
  return { sort: '-timestamp,-created', filter }
}

function loadLogs() {
  page.value = 1
  load(queryOpts())
}

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return logs.value
  return logs.value.filter(l =>
    l.actor_email?.toLowerCase().includes(q) ||
    l.collection_name?.toLowerCase().includes(q) ||
    l.record_id?.toLowerCase().includes(q) ||
    l.request_ip?.toLowerCase().includes(q)
  )
})

const columns: Column<AuditLog>[] = [
  { key: 'timestamp', label: 'Time', format: (v, item) => formatDate(v || item.created, 'PP p') },
  { key: 'event_type', label: 'Action' },
  { key: 'collection_name', label: 'Collection' },
  { key: 'record_id', label: 'Record', mobileLabel: 'Record' },
  { key: 'actor_email', label: 'Actor' },
]

function typeTone(l: AuditLog): SoftTone {
  switch (l.event_type) {
    case 'create': return 'success'
    case 'delete': return 'error'
    case 'auth': return 'info'
    default: return 'neutral'
  }
}

onMounted(loadLogs)
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Audit Log"
    subtitle="Control-plane change history — who edited which policy record, when, and from where."
    search-placeholder="Filter this page by actor, collection, record id, IP..."
    :loading="loading"
    :error="error"
    :is-empty="logs.length === 0"
    :has-query="!!searchQuery || !!typeFilter"
    empty-icon="📜"
    empty-title="No audit entries"
    empty-message="Entries appear once operators create, update, or delete records or sign in."
    error-title="Failed to load audit log"
    @retry="loadLogs"
  >
    <template #toolbar>
      <select v-model="typeFilter" class="select select-bordered sm:w-48" @change="loadLogs">
        <option value="">All actions</option>
        <option v-for="t in TYPES" :key="t" :value="t">{{ formatConstant(t) }}</option>
      </select>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(l) => selected = l">
        <template #cell-event_type="{ item }"><SoftBadge :tone="typeTone(item)" dot>{{ item.event_type || '—' }}</SoftBadge></template>
        <template #card-event_type="{ item }"><SoftBadge :tone="typeTone(item)" dot>{{ item.event_type || '—' }}</SoftBadge></template>
        <template #cell-collection_name="{ item }"><code class="text-xs">{{ item.collection_name || '-' }}</code></template>
        <template #cell-record_id="{ item }"><code class="text-xs">{{ item.record_id || '-' }}</code></template>

        <template #actions="{ item }">
          <button class="btn btn-xs" @click="selected = item">Details</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ logs.length }} of {{ totalItems }} entries
      </ListPagination>
    </BaseCard>
  </ListLayout>

  <!-- Detail modal: actor, request origin, before/after snapshots -->
  <Teleport to="body">
    <dialog class="modal" :class="{ 'modal-open': !!selected }">
      <div class="modal-box max-w-3xl" v-if="selected">
        <div class="flex justify-between items-center mb-4">
          <h3 class="font-bold text-lg flex items-center gap-2">
            <SoftBadge :tone="typeTone(selected)" dot>{{ selected.event_type }}</SoftBadge>
            <code class="text-sm">{{ selected.collection_name }}</code>
          </h3>
          <button @click="selected = null" class="btn btn-sm btn-circle btn-ghost">✕</button>
        </div>

        <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm mb-4">
          <div><span class="opacity-50 text-xs uppercase block">Time</span>{{ formatDate(selected.timestamp || selected.created, 'PPpp') }}</div>
          <div><span class="opacity-50 text-xs uppercase block">Actor</span>{{ selected.actor_email || '-' }}</div>
          <div><span class="opacity-50 text-xs uppercase block">Record</span><code>{{ selected.record_id || '-' }}</code></div>
          <div><span class="opacity-50 text-xs uppercase block">From</span>
            <span class="opacity-70">{{ selected.actor_collection || '-' }}</span>
            <code v-if="selected.request_ip" class="ml-1 text-xs">{{ selected.request_ip }}</code>
          </div>
          <div class="col-span-2"><span class="opacity-50 text-xs uppercase block">Request</span>
            <code class="text-xs">{{ selected.request_method }} {{ selected.request_url || '-' }}</code>
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div class="bg-base-200 rounded-box overflow-hidden">
            <div class="px-3 py-2 text-xs font-medium opacity-60 border-b border-base-300">Before</div>
            <pre class="p-3 text-xs overflow-x-auto"><code>{{ selected.before ? JSON.stringify(selected.before, null, 2) : '—' }}</code></pre>
          </div>
          <div class="bg-base-200 rounded-box overflow-hidden">
            <div class="px-3 py-2 text-xs font-medium opacity-60 border-b border-base-300">After</div>
            <pre class="p-3 text-xs overflow-x-auto"><code>{{ selected.after ? JSON.stringify(selected.after, null, 2) : '—' }}</code></pre>
          </div>
        </div>

        <div class="modal-action">
          <button class="btn" @click="selected = null">Close</button>
        </div>
      </div>
      <div class="modal-backdrop" @click="selected = null"></div>
    </dialog>
  </Teleport>
</template>
