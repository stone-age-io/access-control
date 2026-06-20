<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate } from '@/utils/format'
import type { Schedule, ScheduleWindow } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: schedules, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Schedule>('schedules', 50)
const searchQuery = ref('')
const deleting = ref(false)

const DAY_SHORT = ['', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

function summarize(windows: ScheduleWindow[] | undefined): string {
  if (!windows || windows.length === 0) return 'No windows'
  const w = windows[0]
  const days = (w.days || []).slice().sort((a, b) => a - b).map(d => DAY_SHORT[d] || d).join(',')
  const first = `${days} ${w.start}–${w.end}`
  return windows.length > 1 ? `${first} +${windows.length - 1} more` : first
}

const columns: Column<Schedule>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'windows', label: 'Windows', format: (v) => summarize(v) },
  { key: 'created', label: 'Created', format: (v) => formatDate(v, 'PP') },
]

async function handleDelete(s: Schedule) {
  const confirmed = await confirm({
    title: 'Delete Schedule',
    message: `Delete schedule "${s.code}"?`,
    details: 'Access groups using this schedule will lose their time window. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('schedules').delete(s.id)
    toast.success('Schedule deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete schedule')
  } finally {
    deleting.value = false
  }
}

watchDebounced(searchQuery, reload, { debounce: 300 })
onMounted(reload)
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Schedules"
    subtitle="Reusable weekly time windows. Evaluated in each location's local time."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="schedules.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🗓️"
    empty-title="No schedules yet"
    empty-message="Create a schedule to define when access groups are open."
    error-title="Failed to load schedules"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/schedules/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Schedule</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/schedules/new" class="btn btn-primary">Create Schedule</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="schedules" :columns="columns" :loading="loading" @row-click="(s) => router.push(`/schedules/${s.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>
        <template #cell-windows="{ item }"><span class="text-xs font-mono opacity-80">{{ summarize(item.windows) }}</span></template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/schedules/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ schedules.length }} of {{ totalItems }} schedule(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
