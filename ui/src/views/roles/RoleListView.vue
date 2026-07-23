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
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

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
  <ListLayout
    v-model:search="searchQuery"
    title="Roles"
    subtitle="A named bundle of access groups assigned to cardholders."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="roles.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🛡️"
    empty-title="No roles yet"
    empty-message="Bundle access groups into a role, then assign it to cardholders."
    error-title="Failed to load roles"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/roles/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Role</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/roles/new" class="btn btn-primary">Create Role</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="roles" :columns="columns" :loading="loading" @row-click="(r) => router.push(`/roles/${r.id}`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-access_groups="{ item }">
          <div v-if="groupsOf(item).length" class="flex flex-wrap gap-1">
            <SoftBadge v-for="g in groupsOf(item).slice(0, 3)" :key="g.id" class="font-mono">{{ g.code }}</SoftBadge>
            <SoftBadge v-if="groupsOf(item).length > 3">+{{ groupsOf(item).length - 3 }}</SoftBadge>
          </div>
          <SoftBadge v-else>{{ (item.access_groups || []).length }}</SoftBadge>
        </template>
        <template #card-access_groups="{ item }">
          <div v-if="groupsOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <SoftBadge v-for="g in groupsOf(item).slice(0, 2)" :key="g.id" class="font-mono">{{ g.code }}</SoftBadge>
            <SoftBadge v-if="groupsOf(item).length > 2">+{{ groupsOf(item).length - 2 }}</SoftBadge>
          </div>
          <SoftBadge v-else>{{ (item.access_groups || []).length }}</SoftBadge>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">No matches<template v-if="searchQuery"> for “{{ searchQuery }}”</template>.</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/roles/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ roles.length }} of {{ totalItems }} role(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
