<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate } from '@/utils/format'
import type { Location } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import LocationMapViz from '@/components/locations/LocationMapViz.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: locations, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Location>('locations', 50)
const searchQuery = ref('')
const viewMode = ref<'list' | 'map'>('list')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}" || timezone ~ "${q}"` : ''
  return { sort: 'code', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Location>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'timezone', label: 'Timezone' },
  { key: 'fai_suppress', label: 'FAI Suppress' },
  { key: 'created', label: 'Created', format: (v) => formatDate(v, 'PP') },
]

async function handleDelete(location: Location) {
  const confirmed = await confirm({
    title: 'Delete Location',
    message: `Delete location "${location.code}"?`,
    details: 'Portals referencing this location will be left without one. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('locations').delete(location.id)
    toast.success('Location deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete location')
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
    title="Locations"
    subtitle="Buildings/campuses — each owns a timezone."
    search-placeholder="Search by code, name, or timezone..."
    :loading="loading"
    :error="error"
    :is-empty="locations.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🏢"
    empty-title="No locations yet"
    empty-message="Create your first location to anchor portals and timezones."
    error-title="Failed to load locations"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/locations/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Location</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/locations/new" class="btn btn-primary">Create Location</router-link>
    </template>

    <template #toolbar>
      <div class="join">
        <button
          class="join-item btn btn-sm"
          :class="viewMode === 'list' ? 'btn-active btn-primary' : ''"
          @click="viewMode = 'list'"
        >
          ☰ <span class="hidden sm:inline">List</span>
        </button>
        <button
          class="join-item btn btn-sm"
          :class="viewMode === 'map' ? 'btn-active btn-primary' : ''"
          @click="viewMode = 'map'"
        >
          🗺️ <span class="hidden sm:inline">Map</span>
        </button>
      </div>
    </template>

    <LocationMapViz v-if="viewMode === 'map'" :search-query="searchQuery" />

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="locations" :columns="columns" :loading="loading" @row-click="(l) => router.push(`/locations/${l.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-fai_suppress="{ item }">
          <SoftBadge :tone="item.fai_suppress ? 'success' : 'neutral'" dot>
            {{ item.fai_suppress ? 'on' : 'off' }}
          </SoftBadge>
        </template>
        <template #card-fai_suppress="{ item }">
          <SoftBadge :tone="item.fai_suppress ? 'success' : 'neutral'" dot>
            {{ item.fai_suppress ? 'on' : 'off' }}
          </SoftBadge>
        </template>

        <template #cell-timezone="{ item }"><span class="font-mono text-xs">{{ item.timezone }}</span></template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">No matches<template v-if="searchQuery"> for “{{ searchQuery }}”</template>.</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/locations/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ locations.length }} of {{ totalItems }} location(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
