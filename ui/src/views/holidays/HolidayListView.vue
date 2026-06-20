<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate } from '@/utils/format'
import type { Holiday } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: holidays, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Holiday>('holidays', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `name ~ "${q}"` : ''
  return { sort: 'date', filter, expand: 'location' }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Holiday>[] = [
  { key: 'name', label: 'Name' },
  { key: 'date', label: 'Date', format: (v) => formatDate(v, 'PP') },
  { key: 'recurring', label: 'Recurring' },
  { key: 'location', label: 'Location' },
]

async function handleDelete(h: Holiday) {
  const confirmed = await confirm({
    title: 'Delete Holiday',
    message: `Delete holiday "${h.name || h.date}"?`,
    details: 'Schedules that observe holidays will reopen on this day. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('holidays').delete(h.id)
    toast.success('Holiday deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete holiday')
  } finally {
    deleting.value = false
  }
}

watchDebounced(searchQuery, reload, { debounce: 300 })
onMounted(reload)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Holidays</h1>
        <p class="text-base-content/70 mt-1">Days a location is closed — they close every window of any holiday-observing schedule.</p>
      </div>
      <router-link to="/holidays/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Holiday</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by name..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && holidays.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && holidays.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load holidays</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="holidays.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">📅</span>
        <h3 class="text-xl font-bold mt-4">No holidays yet</h3>
        <p class="text-base-content/70 mt-2">Add a holiday to close access on a specific date.</p>
        <router-link to="/holidays/new" class="btn btn-primary mt-4">Create Holiday</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="holidays" :columns="columns" :loading="loading" @row-click="(h) => router.push(`/holidays/${h.id}`)">
        <template #cell-name="{ item }"><span class="font-bold">{{ item.name || '—' }}</span></template>
        <template #card-name="{ item }"><span class="font-bold">{{ item.name || '—' }}</span></template>

        <template #cell-date="{ item }"><span class="font-mono text-xs">{{ formatDate(item.date, 'PP') }}</span></template>

        <template #cell-recurring="{ item }">
          <span class="badge badge-sm" :class="item.recurring ? 'badge-success' : 'badge-ghost'">
            {{ item.recurring ? 'yearly' : 'once' }}
          </span>
        </template>
        <template #card-recurring="{ item }">
          <span class="badge badge-sm" :class="item.recurring ? 'badge-success' : 'badge-ghost'">
            {{ item.recurring ? 'yearly' : 'once' }}
          </span>
        </template>

        <template #cell-location="{ item }">
          <code class="text-xs">{{ item.expand?.location?.code || '—' }}</code>
        </template>
        <template #card-location="{ item }">
          <code class="text-xs">{{ item.expand?.location?.code || '—' }}</code>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/holidays/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ holidays.length }} of {{ totalItems }} holiday(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
