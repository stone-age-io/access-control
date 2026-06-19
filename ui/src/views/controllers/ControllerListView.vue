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
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Controllers</h1>
        <p class="text-base-content/70 mt-1">Edge boxes that drive the portals assigned to them.</p>
      </div>
      <router-link to="/controllers/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Controller</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code or name..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && controllers.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && controllers.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load controllers</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="controllers.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">⚙️</span>
        <h3 class="text-xl font-bold mt-4">No controllers yet</h3>
        <p class="text-base-content/70 mt-2">Register an edge box, then assign portals to it.</p>
        <router-link to="/controllers/new" class="btn btn-primary mt-4">Create Controller</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
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
          <span class="badge badge-ghost badge-sm font-mono">{{ item.model || '-' }}</span>
        </template>
        <template #card-model="{ item }">
          <span class="badge badge-ghost badge-sm font-mono">{{ item.model || '-' }}</span>
        </template>

        <template #cell-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'online' ? 'badge-success' : 'badge-ghost'">{{ item.status || 'unknown' }}</span>
        </template>
        <template #card-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'online' ? 'badge-success' : 'badge-ghost'">{{ item.status || 'unknown' }}</span>
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
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/controllers/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ controllers.length }} of {{ totalItems }} controller(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
