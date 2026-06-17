<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { AccessGroup, AccessPoint } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: groups, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AccessGroup>('access_groups', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', expand: 'access_points,schedule', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<AccessGroup>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'expand.schedule.code', label: 'Schedule' },
  { key: 'access_points', label: 'Points' },
]

function pointsOf(g: AccessGroup): AccessPoint[] {
  return g.expand?.access_points || []
}

async function handleDelete(g: AccessGroup) {
  const confirmed = await confirm({
    title: 'Delete Access Group',
    message: `Delete access group "${g.code}"?`,
    details: 'Roles referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('access_groups').delete(g.id)
    toast.success('Access group deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete access group')
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
        <h1 class="text-3xl font-bold">Access Groups</h1>
        <p class="text-base-content/70 mt-1">A set of access points opened under one schedule (an "access level").</p>
      </div>
      <router-link to="/access-groups/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Access Group</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code or name..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && groups.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && groups.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load access groups</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="groups.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🗝️</span>
        <h3 class="text-xl font-bold mt-4">No access groups yet</h3>
        <p class="text-base-content/70 mt-2">Group access points under a schedule, then assign groups to roles.</p>
        <router-link to="/access-groups/new" class="btn btn-primary mt-4">Create Access Group</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="groups" :columns="columns" :loading="loading" @row-click="(g) => router.push(`/access-groups/${g.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-expand.schedule.code="{ item }">
          <code
            v-if="item.expand?.schedule"
            class="text-xs link link-hover"
            @click.stop="router.push(`/schedules/${item.expand.schedule.id}`)"
          >{{ item.expand.schedule.code }}</code>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-expand.schedule.code="{ item }">
          <code v-if="item.expand?.schedule" class="text-xs">{{ item.expand.schedule.code }}</code>
          <span v-else>-</span>
        </template>

        <template #cell-access_points="{ item }">
          <div v-if="pointsOf(item).length" class="flex flex-wrap gap-1">
            <code v-for="p in pointsOf(item).slice(0, 3)" :key="p.id" class="badge badge-ghost badge-sm">{{ p.code }}</code>
            <span v-if="pointsOf(item).length > 3" class="badge badge-ghost badge-sm">+{{ pointsOf(item).length - 3 }}</span>
          </div>
          <span v-else-if="(item.access_points || []).length" class="badge badge-ghost badge-sm">{{ (item.access_points || []).length }}</span>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-access_points="{ item }">
          <div v-if="pointsOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <code v-for="p in pointsOf(item).slice(0, 2)" :key="p.id" class="badge badge-ghost badge-sm">{{ p.code }}</code>
            <span v-if="pointsOf(item).length > 2" class="badge badge-ghost badge-sm">+{{ pointsOf(item).length - 2 }}</span>
          </div>
          <span v-else-if="(item.access_points || []).length" class="badge badge-ghost badge-sm">{{ (item.access_points || []).length }}</span>
          <span v-else>-</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/access-groups/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ groups.length }} of {{ totalItems }} access group(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
