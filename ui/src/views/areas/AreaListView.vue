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

const canCommand = computed(() => auth.can('command'))
const { commanding, arm, disarm, armClear } = useAreaCommands()

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
  page.value = 1
  load(queryOpts())
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

    <BaseCard :no-padding="true">
      <ResponsiveList :items="items" :columns="columns" :loading="loading" @row-click="(a) => router.push(`/areas/${a.id}`)">
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
          <template v-if="canCommand">
            <button class="btn btn-xs btn-warning" :disabled="commanding" @click="arm(item.id, item.code)">Arm</button>
            <button class="btn btn-xs" :disabled="commanding" @click="disarm(item.id, item.code)">Disarm</button>
            <button class="btn btn-xs btn-ghost" :disabled="commanding || !item.arm_override" @click="armClear(item.id)">Clear</button>
          </template>
          <router-link :to="`/areas/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ items.length }} of {{ totalItems }} area(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
