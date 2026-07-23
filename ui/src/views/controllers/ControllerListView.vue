<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate, formatRelativeTime } from '@/utils/format'
import type { Controller } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: controllers, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Controller>('controllers', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', expand: 'location', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Controller>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'expand.location.code', label: 'Location' },
  { key: 'model', label: 'Model' },
  { key: 'status', label: 'Status' },
  { key: 'last_seen', label: 'Last seen' },
]

async function handleDelete(c: Controller) {
  const confirmed = await confirm({
    title: 'Delete Controller',
    message: `Delete controller "${c.code}"?`,
    details: 'Portals assigned to it will become unassigned and will not be armed until reassigned. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('controllers').delete(c.id)
    toast.success('Controller deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete controller')
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
    title="Controllers"
    subtitle="Edge boxes that drive the portals assigned to them."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="controllers.length === 0"
    :has-query="!!searchQuery"
    empty-icon="⚙️"
    empty-title="No controllers yet"
    empty-message="Register an edge box, then assign portals to it."
    error-title="Failed to load controllers"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/controllers/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Controller</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/controllers/new" class="btn btn-primary">Create Controller</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="controllers" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/controllers/${c.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

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

        <template #cell-model="{ item }">
          <SoftBadge class="font-mono">{{ item.model || '-' }}</SoftBadge>
        </template>
        <template #card-model="{ item }">
          <SoftBadge class="font-mono">{{ item.model || '-' }}</SoftBadge>
        </template>

        <template #cell-status="{ item }">
          <SoftBadge :tone="item.status === 'online' ? 'success' : 'neutral'" dot>{{ item.status || 'unknown' }}</SoftBadge>
        </template>
        <template #card-status="{ item }">
          <SoftBadge :tone="item.status === 'online' ? 'success' : 'neutral'" dot>{{ item.status || 'unknown' }}</SoftBadge>
        </template>

        <template #cell-last_seen="{ item }">
          <span v-if="item.last_seen" class="text-xs" :title="formatDate(item.last_seen)">{{ formatRelativeTime(item.last_seen) }}</span>
          <span v-else class="text-base-content/40">never</span>
        </template>
        <template #card-last_seen="{ item }">
          <span v-if="item.last_seen" class="text-xs" :title="formatDate(item.last_seen)">{{ formatRelativeTime(item.last_seen) }}</span>
          <span v-else class="text-base-content/40">never</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">No matches<template v-if="searchQuery"> for “{{ searchQuery }}”</template>.</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/controllers/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ controllers.length }} of {{ totalItems }} controller(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
