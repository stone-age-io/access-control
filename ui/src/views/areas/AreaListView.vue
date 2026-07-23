<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { useAreaCommands } from '@/composables/useAreaCommands'
import { aggregateArm, armBadge, armLabel } from '@/utils/arming'
import { pb } from '@/utils/pb'
import type { Area, PointStatus } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()

const { items, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Area>('areas', 50)
const searchQuery = ref('')
const deleting = ref(false)

// Only operators with the `command` capability get selection + the command bar;
// the API enforces it too, so there is no point letting others select.
const canCommand = computed(() => auth.can('command'))
const { commanding, armBulk, disarmBulk, clearBulk } = useAreaCommands()

// Bulk selection — ids can span pages, but we clear it whenever the page/query
// changes so the command bar never acts on areas the operator can't see.
const selectedIds = ref<string[]>([])

// Live arm-state per area from the point_status shadow. An area spans one shadow row
// PER participating controller (same code, distinct controller), so we key by code to a
// LIST and aggregate — unlike portals, which have a single row per code. Loaded once and
// kept live; independent of search/pagination.
const shadowsByCode = ref<Map<string, PointStatus[]>>(new Map())
let unsubStatus: (() => void) | null = null

function armFor(a: Area) {
  return aggregateArm(shadowsByCode.value.get(a.code) ?? [])
}

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', filter, expand: 'location' }
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

async function loadShadows() {
  try {
    const rows = await pb.collection('point_status').getFullList<PointStatus>({ filter: 'kind = "area"' })
    const m = new Map<string, PointStatus[]>()
    for (const r of rows) m.set(r.code, [...(m.get(r.code) ?? []), r])
    shadowsByCode.value = m
  } catch {
    shadowsByCode.value = new Map()
  }
}

async function subscribeShadows() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    if (e.record.kind !== 'area') return
    const m = new Map(shadowsByCode.value)
    const arr = (m.get(e.record.code) ?? []).filter((s) => s.key !== e.record.key)
    if (e.action !== 'delete') arr.push(e.record)
    if (arr.length) m.set(e.record.code, arr)
    else m.delete(e.record.code)
    shadowsByCode.value = m
  })
}

const columns: Column<Area>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'location', label: 'Location' },
  { key: 'state', label: 'State' },
  { key: 'arm', label: 'Standing' },
  { key: 'arm_override', label: 'Override' },
]

async function bulkArm() {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  const n = ids.length
  const confirmed = await confirm({
    title: 'Arm areas',
    message: `Arm ${n} area${n > 1 ? 's' : ''}?`,
    details: 'Armed intrusion points raise alarms until disarmed. This persists across controller reboots.',
    confirmText: `Arm ${n} area${n > 1 ? 's' : ''}`,
    variant: 'warning',
  })
  if (!confirmed) return
  await armBulk(ids)
  selectedIds.value = []
}

async function bulkDisarm() {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  const n = ids.length
  const confirmed = await confirm({
    title: 'Disarm areas',
    message: `Disarm ${n} area${n > 1 ? 's' : ''}?`,
    details: 'Intrusion points on each selected area will stop raising alarms.',
    confirmText: `Disarm ${n} area${n > 1 ? 's' : ''}`,
    variant: 'warning',
  })
  if (!confirmed) return
  await disarmBulk(ids)
  selectedIds.value = []
}

async function bulkClear() {
  const ids = [...selectedIds.value]
  if (!ids.length) return
  const n = ids.length
  const confirmed = await confirm({
    title: 'Clear overrides',
    message: `Clear the arm override on ${n} area${n > 1 ? 's' : ''}?`,
    details: 'Reverts each selected area to its scheduled or standing arm-state.',
    confirmText: 'Clear overrides',
    variant: 'info',
  })
  if (!confirmed) return
  await clearBulk(ids)
  selectedIds.value = []
}

async function handleDelete(a: Area) {
  const confirmed = await confirm({
    title: 'Delete Area',
    message: `Delete area "${a.code}"?`,
    details: 'Member inputs keep their wiring but stop arming. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('areas').delete(a.id)
    toast.success('Area deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete area')
  } finally {
    deleting.value = false
  }
}

watchDebounced(searchQuery, reload, { debounce: 300 })
onMounted(() => {
  reload()
  loadShadows()
  subscribeShadows()
})
onBeforeUnmount(() => {
  if (unsubStatus) unsubStatus()
})
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Areas"
    subtitle="Arm-state groupings for intrusion-lite. Membership is set on each aux input."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="items.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🛡️"
    empty-title="No areas yet"
    empty-message="Create an area, then assign intrusion inputs to it."
    error-title="Failed to load areas"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/areas/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Area</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/areas/new" class="btn btn-primary">Create Area</router-link>
    </template>

    <!-- Bulk command bar — appears when areas are selected (command capability only).
         Arm/disarm are live commands, deliberately kept out of the per-row actions
         (which stay for record management: Edit / Delete). -->
    <div
      v-if="canCommand && selectedIds.length"
      class="sticky top-2 z-20 flex flex-wrap items-center gap-2 rounded-xl border border-primary/30 bg-base-100/95 p-3 shadow-md backdrop-blur"
    >
      <span class="text-sm font-bold mr-1">{{ selectedIds.length }} selected</span>
      <button class="btn btn-xs btn-warning" :disabled="commanding" @click="bulkArm">Arm</button>
      <button class="btn btn-xs" :disabled="commanding" @click="bulkDisarm">Disarm</button>
      <button class="btn btn-xs btn-ghost" :disabled="commanding" @click="bulkClear">Clear override</button>
      <button class="btn btn-xs btn-ghost ml-auto" :disabled="commanding" @click="selectedIds = []">Cancel</button>
    </div>

    <BaseCard :no-padding="true">
      <ResponsiveList
        :items="items"
        :columns="columns"
        :loading="loading"
        :selectable="canCommand"
        v-model:selected="selectedIds"
        @row-click="(a) => router.push(`/areas/${a.id}`)"
      >
        <template #cell-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>

        <template #cell-name="{ item }">{{ item.name || '—' }}</template>
        <template #card-name="{ item }">{{ item.name || '—' }}</template>

        <template #cell-location="{ item }"><code class="text-xs">{{ item.expand?.location?.code || '—' }}</code></template>
        <template #card-location="{ item }"><code class="text-xs">{{ item.expand?.location?.code || '—' }}</code></template>

        <!-- Live aggregated arm-state across this area's controllers (Unknown = none reporting). -->
        <template #cell-state="{ item }">
          <span class="badge badge-sm" :class="armBadge(armFor(item).state)">{{ armLabel(armFor(item)) }}</span>
        </template>
        <template #card-state="{ item }">
          <span class="badge badge-sm" :class="armBadge(armFor(item).state)">{{ armLabel(armFor(item)) }}</span>
        </template>

        <template #cell-arm="{ item }">
          <span class="badge badge-sm" :class="item.arm === 'armed' ? 'badge-error' : 'badge-ghost'">{{ item.arm || 'disarmed' }}</span>
        </template>
        <template #card-arm="{ item }">
          <span class="badge badge-sm" :class="item.arm === 'armed' ? 'badge-error' : 'badge-ghost'">{{ item.arm || 'disarmed' }}</span>
        </template>

        <template #cell-arm_override="{ item }">
          <span v-if="item.arm_override" class="badge badge-sm badge-warning">{{ item.arm_override }}</span>
          <span v-else class="opacity-40">—</span>
        </template>
        <template #card-arm_override="{ item }">
          <span v-if="item.arm_override" class="badge badge-sm badge-warning">{{ item.arm_override }}</span>
          <span v-else class="opacity-40">—</span>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/areas/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="goPrev" @next="goNext">
        {{ items.length }} of {{ totalItems }} area(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
