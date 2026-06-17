<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { Role, AccessGroup } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: roles, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Role>('roles', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', expand: 'access_groups', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Role>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'access_groups', label: 'Groups' },
]

function groupsOf(r: Role): AccessGroup[] {
  return r.expand?.access_groups || []
}

async function handleDelete(r: Role) {
  const confirmed = await confirm({
    title: 'Delete Role',
    message: `Delete role "${r.code}"?`,
    details: 'Cardholders referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('roles').delete(r.id)
    toast.success('Role deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete role')
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
        <h1 class="text-3xl font-bold">Roles</h1>
        <p class="text-base-content/70 mt-1">A named bundle of access groups assigned to cardholders.</p>
      </div>
      <router-link to="/roles/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Role</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code or name..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && roles.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && roles.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load roles</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="roles.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🛡️</span>
        <h3 class="text-xl font-bold mt-4">No roles yet</h3>
        <p class="text-base-content/70 mt-2">Bundle access groups into a role, then assign it to cardholders.</p>
        <router-link to="/roles/new" class="btn btn-primary mt-4">Create Role</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="roles" :columns="columns" :loading="loading" @row-click="(r) => router.push(`/roles/${r.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-access_groups="{ item }">
          <div v-if="groupsOf(item).length" class="flex flex-wrap gap-1">
            <code v-for="g in groupsOf(item).slice(0, 3)" :key="g.id" class="badge badge-ghost badge-sm">{{ g.code }}</code>
            <span v-if="groupsOf(item).length > 3" class="badge badge-ghost badge-sm">+{{ groupsOf(item).length - 3 }}</span>
          </div>
          <span v-else class="badge badge-ghost badge-sm">{{ (item.access_groups || []).length }}</span>
        </template>
        <template #card-access_groups="{ item }">
          <div v-if="groupsOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <code v-for="g in groupsOf(item).slice(0, 2)" :key="g.id" class="badge badge-ghost badge-sm">{{ g.code }}</code>
            <span v-if="groupsOf(item).length > 2" class="badge badge-ghost badge-sm">+{{ groupsOf(item).length - 2 }}</span>
          </div>
          <span v-else class="badge badge-ghost badge-sm">{{ (item.access_groups || []).length }}</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/roles/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ roles.length }} of {{ totalItems }} role(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
