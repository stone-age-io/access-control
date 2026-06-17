<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate } from '@/utils/format'
import type { Schedule, ScheduleWindow } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: schedules, totalItems, loading, error, load } = usePagination<Schedule>('schedules', 50)
const searchQuery = ref('')
const deleting = ref(false)

const DAY_SHORT = ['', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return schedules.value
  return schedules.value.filter(s => s.code?.toLowerCase().includes(q) || s.name?.toLowerCase().includes(q))
})

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

function loadSchedules() {
  load({ sort: 'code' })
}

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
    loadSchedules()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete schedule')
  } finally {
    deleting.value = false
  }
}

onMounted(loadSchedules)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Schedules</h1>
        <p class="text-base-content/70 mt-1">Reusable weekly time windows. Evaluated in each site's local time.</p>
      </div>
      <router-link to="/schedules/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Schedule</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code or name..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && schedules.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && schedules.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load schedules</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="loadSchedules" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="schedules.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">🗓️</span>
        <h3 class="text-xl font-bold mt-4">No schedules yet</h3>
        <p class="text-base-content/70 mt-2">Create a schedule to define when access groups are open.</p>
        <router-link to="/schedules/new" class="btn btn-primary mt-4">Create Schedule</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(s) => router.push(`/schedules/${s.id}/edit`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>
        <template #cell-windows="{ item }"><span class="text-xs font-mono opacity-80">{{ summarize(item.windows) }}</span></template>

        <template #actions="{ item }">
          <router-link :to="`/schedules/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>
      <div class="p-4 border-t border-base-300 text-sm text-base-content/60">{{ totalItems }} schedule(s)</div>
    </BaseCard>
  </div>
</template>
