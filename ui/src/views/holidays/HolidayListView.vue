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
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

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
  <ListLayout
    v-model:search="searchQuery"
    title="Holidays"
    subtitle="Days a location is closed — they close every window of any holiday-observing schedule."
    search-placeholder="Search by name..."
    :loading="loading"
    :error="error"
    :is-empty="holidays.length === 0"
    :has-query="!!searchQuery"
    empty-icon="📅"
    empty-title="No holidays yet"
    empty-message="Add a holiday to close access on a specific date."
    error-title="Failed to load holidays"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/holidays/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Holiday</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/holidays/new" class="btn btn-primary">Create Holiday</router-link>
    </template>

    <BaseCard :no-padding="true">
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

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ holidays.length }} of {{ totalItems }} holiday(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
