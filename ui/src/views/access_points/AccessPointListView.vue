<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { AccessPoint } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: points, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessPoint>('access_points', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', expand: 'site', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<AccessPoint>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'expand.site.code', label: 'Site' },
  { key: 'posture', label: 'Posture' },
  { key: 'pulse_seconds', label: 'Pulse', mobileLabel: 'Pulse (s)' },
]

async function handleDelete(p: AccessPoint) {
  const confirmed = await confirm({
    title: 'Delete Access Point',
    message: `Delete access point "${p.code}"?`,
    details: 'Access groups referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('access_points').delete(p.id)
    toast.success('Access point deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete access point')
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
        <h1 class="text-3xl font-bold">Access Points</h1>
        <p class="text-base-content/70 mt-1">Controllable openings — doors, gates, turnstiles, elevators.</p>
      </div>
      <router-link to="/access-points/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Access Point</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code, name, or site..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && points.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && points.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load access points</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="points.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🚪</span>
        <h3 class="text-xl font-bold mt-4">No access points yet</h3>
        <p class="text-base-content/70 mt-2">Add an access point and assign it to a site.</p>
        <router-link to="/access-points/new" class="btn btn-primary mt-4">Create Access Point</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="points" :columns="columns" :loading="loading" @row-click="(p) => router.push(`/access-points/${p.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-expand.site.code="{ item }">
          <router-link v-if="item.expand?.site" :to="`/sites/${item.expand.site.id}`" class="link link-hover" @click.stop>
            <code class="text-xs">{{ item.expand.site.code }}</code>
          </router-link>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-expand.site.code="{ item }">
          <router-link v-if="item.expand?.site" :to="`/sites/${item.expand.site.id}`" class="link link-hover" @click.stop>
            <code class="text-xs">{{ item.expand.site.code }}</code>
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
          <router-link :to="`/access-points/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ points.length }} of {{ totalItems }} access point(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
