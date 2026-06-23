<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { usePortalCommands, POSTURES } from '@/composables/usePortalCommands'
import { pb } from '@/utils/pb'
import type { Portal, PointStatus } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import PostureBadge from '@/components/ui/PostureBadge.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()

const { items: portals, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Portal>('portals', 50)
const searchQuery = ref('')
const deleting = ref(false)

// Only operators with the `command` capability get selection + the command bar;
// the API enforces it too, so there is no point letting others select.
const canCommand = computed(() => auth.can('command'))
const { commanding, setPostureBulk } = usePortalCommands()

// Live "device shadow" per portal (effective posture / door state / override),
// keyed by code — same projection the floor plan watches. Loaded once and kept
// live; it is independent of search/pagination.
const statusByCode = ref<Map<string, PointStatus>>(new Map())
let unsubStatus: (() => void) | null = null

// Bulk selection — ids can span pages, but we clear it whenever the page/query
// changes so the command bar never acts on portals the operator can't see.
const selectedIds = ref<string[]>([])

function statusFor(p: Portal): PointStatus | undefined {
  return statusByCode.value.get(p.code)
}

function doorBadge(p: Portal): { cls: string; text: string } {
  switch (statusFor(p)?.state) {
    case 'open':
      return { cls: 'badge-error', text: 'Open' }
    case 'closed':
      return { cls: 'badge-success', text: 'Closed' }
    default:
      return { cls: 'badge-ghost', text: 'Unknown' }
  }
}

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', expand: 'location', filter }
}

function reload() {
  selectedIds.value = []
  page.value = 1
  load(queryOpts())
}
function goNext() {
  selectedIds.value = []
  nextPage(queryOpts())
}
function goPrev() {
  selectedIds.value = []
  prevPage(queryOpts())
}

const columns: Column<Portal>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'type', label: 'Type' },
  { key: 'expand.location.code', label: 'Location' },
  { key: 'door', label: 'Door' },
  { key: 'posture', label: 'Posture' },
]

async function loadStatuses() {
  try {
    const rows = await pb.collection('point_status').getFullList<PointStatus>({ filter: 'kind = "portal"' })
    const m = new Map<string, PointStatus>()
    for (const r of rows) m.set(r.code, r)
    statusByCode.value = m
  } catch {
    statusByCode.value = new Map()
  }
}

async function subscribeStatus() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    if (e.record.kind !== 'portal') return
    const m = new Map(statusByCode.value)
    if (e.action === 'delete') m.delete(e.record.code)
    else m.set(e.record.code, e.record)
    statusByCode.value = m
  })
}

async function bulkPosture(p: { value: string; label: string; danger?: boolean }) {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  const n = ids.length
  const confirmed = await confirm({
    title: `Set posture: ${p.label}`,
    message: `Set ${n} portal${n > 1 ? 's' : ''} to ${p.value}?`,
    details:
      p.value === 'lockdown'
        ? 'Lockdown denies all access on every selected portal, beating any valid credential, until cleared.'
        : p.value === 'disabled'
          ? 'Disabled stops enforcement on every selected portal until cleared.'
          : undefined,
    confirmText: `Set ${n} portal${n > 1 ? 's' : ''}`,
    variant: p.danger ? 'warning' : 'info',
  })
  if (!confirmed) return
  await setPostureBulk(ids, p.value)
  selectedIds.value = []
}

async function bulkClear() {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  const n = ids.length
  const confirmed = await confirm({
    title: 'Clear overrides',
    message: `Clear the manual override on ${n} portal${n > 1 ? 's' : ''}?`,
    details: 'Reverts each selected portal to its scheduled or standing posture.',
    confirmText: 'Clear overrides',
    variant: 'info',
  })
  if (!confirmed) return
  await setPostureBulk(ids, 'clear')
  selectedIds.value = []
}

async function handleDelete(p: Portal) {
  const confirmed = await confirm({
    title: 'Delete Portal',
    message: `Delete portal "${p.code}"?`,
    details: 'Access groups referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('portals').delete(p.id)
    toast.success('Portal deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete portal')
  } finally {
    deleting.value = false
  }
}

watchDebounced(searchQuery, reload, { debounce: 300 })
onMounted(() => {
  reload()
  loadStatuses()
  subscribeStatus()
})
onBeforeUnmount(() => {
  if (unsubStatus) unsubStatus()
})
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Portals"
    subtitle="Controllable openings — doors, gates, turnstiles, elevators."
    search-placeholder="Search by code, name, or location..."
    :loading="loading"
    :error="error"
    :is-empty="portals.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🚪"
    empty-title="No portals yet"
    empty-message="Add a portal and assign it to a location."
    error-title="Failed to load portals"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/portals/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Portal</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/portals/new" class="btn btn-primary">Create Portal</router-link>
    </template>

    <!-- Bulk command bar — appears when portals are selected (command capability only). -->
    <div
      v-if="canCommand && selectedIds.length"
      class="sticky top-2 z-20 flex flex-wrap items-center gap-2 rounded-xl border border-primary/30 bg-base-100/95 p-3 shadow-md backdrop-blur"
    >
      <span class="text-sm font-bold mr-1">{{ selectedIds.length }} selected</span>
      <span class="text-[10px] uppercase font-bold opacity-50 tracking-wide">Set posture:</span>
      <button
        v-for="p in POSTURES"
        :key="p.value"
        class="btn btn-xs"
        :class="p.danger ? 'btn-outline btn-warning' : 'btn-outline'"
        :disabled="commanding"
        @click="bulkPosture(p)"
      >
        {{ p.label }}
      </button>
      <button class="btn btn-xs btn-ghost" :disabled="commanding" @click="bulkClear">Clear override</button>
      <button class="btn btn-xs btn-ghost ml-auto" :disabled="commanding" @click="selectedIds = []">Cancel</button>
    </div>

    <BaseCard :no-padding="true">
      <ResponsiveList
        :items="portals"
        :columns="columns"
        :loading="loading"
        :selectable="canCommand"
        v-model:selected="selectedIds"
        @row-click="(p) => router.push(`/portals/${p.id}`)"
      >
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>
        <template #card-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>

        <template #cell-expand.location.code="{ item }">
          <router-link v-if="item.expand?.location" :to="`/locations/${item.expand.location.id}`" class="link link-hover" @click.stop>
            <code class="text-xs">{{ item.expand.location.code }}</code>
          </router-link>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-expand.location.code="{ item }">
          <router-link v-if="item.expand?.location" :to="`/locations/${item.expand.location.id}`" class="link link-hover" @click.stop>
            <code class="text-xs">{{ item.expand.location.code }}</code>
          </router-link>
          <span v-else>-</span>
        </template>

        <!-- Live door state from the point_status shadow (Unknown = not reporting). -->
        <template #cell-door="{ item }">
          <span class="inline-flex items-center gap-1">
            <span class="badge badge-sm" :class="doorBadge(item).cls">{{ doorBadge(item).text }}</span>
            <span v-if="statusFor(item)?.held" class="badge badge-xs badge-warning">Held</span>
          </span>
        </template>
        <template #card-door="{ item }">
          <span class="inline-flex items-center gap-1">
            <span class="badge badge-sm" :class="doorBadge(item).cls">{{ doorBadge(item).text }}</span>
            <span v-if="statusFor(item)?.held" class="badge badge-xs badge-warning">Held</span>
          </span>
        </template>

        <!-- Live effective posture (with override provenance) when reporting; else the
             configured standing posture, dimmed, so the column never looks live when it isn't. -->
        <template #cell-posture="{ item }">
          <PostureBadge v-if="statusFor(item)" :posture="statusFor(item)!.posture" :source="statusFor(item)!.posture_source" />
          <span v-else class="badge badge-ghost badge-sm opacity-60" title="No live status — showing configured standing posture">
            {{ item.posture || 'secure' }}
          </span>
        </template>
        <template #card-posture="{ item }">
          <PostureBadge v-if="statusFor(item)" :posture="statusFor(item)!.posture" :source="statusFor(item)!.posture_source" />
          <span v-else class="badge badge-ghost badge-sm opacity-60" title="No live status — showing configured standing posture">
            {{ item.posture || 'secure' }}
          </span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/portals/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="goPrev" @next="goNext">
        {{ portals.length }} of {{ totalItems }} portal(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
