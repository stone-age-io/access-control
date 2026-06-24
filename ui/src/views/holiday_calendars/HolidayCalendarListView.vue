<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { HolidayCalendar } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: calendars, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<HolidayCalendar>('holiday_calendars', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<HolidayCalendar>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
]

async function handleDelete(c: HolidayCalendar) {
  const confirmed = await confirm({
    title: 'Delete Calendar',
    message: `Delete holiday calendar "${c.code}"?`,
    details: 'Its holidays are removed and any location that observes it stops closing on those dates. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('holiday_calendars').delete(c.id)
    toast.success('Calendar deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete calendar')
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
    title="Holiday Calendars"
    subtitle="Shareable sets of holiday dates. A location observes one or more — so one “Christmas” serves every site."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="calendars.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🗓️"
    empty-title="No holiday calendars yet"
    empty-message="Create a calendar (e.g. “US Holidays”), add its dates, then have locations observe it."
    error-title="Failed to load calendars"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/holiday-calendars/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Calendar</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/holiday-calendars/new" class="btn btn-primary">Create Calendar</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="calendars" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/holiday-calendars/${c.id}`)">
        <template #cell-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>

        <template #cell-name="{ item }">{{ item.name || '—' }}</template>
        <template #card-name="{ item }">{{ item.name || '—' }}</template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/holiday-calendars/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ calendars.length }} of {{ totalItems }} calendar(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
