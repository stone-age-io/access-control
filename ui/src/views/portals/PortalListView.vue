<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { Portal } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: portals, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Portal>('portals', 50)
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

const columns: Column<Portal>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'type', label: 'Type' },
  { key: 'expand.location.code', label: 'Location' },
  { key: 'posture', label: 'Posture' },
  { key: 'pulse_seconds', label: 'Pulse', mobileLabel: 'Pulse (s)' },
]

async function handleDelete(p: Portal) {
  const confirmed = await confirm({
    title: 'Delete Portal',
    message: `Delete portal "${p.code}"?`,
    details: 'Access groups referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('portals').delete(p.id)
    toast.success('Portal deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete portal')
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
        <h1 class="text-3xl font-bold">Portals</h1>
        <p class="text-base-content/70 mt-1">Controllable openings — doors, gates, turnstiles, elevators.</p>
      </div>
      <router-link to="/portals/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Portal</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code, name, or location..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && portals.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && portals.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load portals</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="portals.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🚪</span>
        <h3 class="text-xl font-bold mt-4">No portals yet</h3>
        <p class="text-base-content/70 mt-2">Add a portal and assign it to a location.</p>
        <router-link to="/portals/new" class="btn btn-primary mt-4">Create Portal</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="portals" :columns="columns" :loading="loading" @row-click="(p) => router.push(`/portals/${p.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>
        <template #card-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>

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

        <template #cell-posture="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.posture || 'secure' }}</span>
        </template>
        <template #card-posture="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.posture || 'secure' }}</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/portals/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ portals.length }} of {{ totalItems }} portal(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
