<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { pb } from '@/utils/pb'
import type { User } from '@/types/pocketbase'
import { presetLabel } from '@/utils/capabilities'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const authStore = useAuthStore()

const { items: operators, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<User>('users', 50)
const searchQuery = ref('')
const deleting = ref(false)

// The Notify column conveys both the opt-in and its location scope at a glance:
// off, all sites (empty scope), or a count when narrowed to specific locations.
function notifyLabel(u: User): string {
  if (!u.notify) return 'no'
  const n = u.notify_locations?.length ?? 0
  return n ? `${n} site${n > 1 ? 's' : ''}` : 'all sites'
}

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `email ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'email', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<User>[] = [
  { key: 'email', label: 'Email' },
  { key: 'name', label: 'Name' },
  { key: 'permissions', label: 'Permissions' },
  { key: 'verified', label: 'Verified' },
  { key: 'notify', label: 'Notify' },
]

async function handleDelete(u: User) {
  if (u.id === authStore.user?.id) {
    toast.error('You cannot delete your own account.')
    return
  }
  const confirmed = await confirm({
    title: 'Delete Operator',
    message: `Delete operator "${u.email}"?`,
    details: 'They will no longer be able to sign in to the management UI. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('users').delete(u.id)
    toast.success('Operator deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete operator')
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
    title="Operators"
    subtitle="Management-UI accounts and their permissions. Superusers sign in at the PocketBase dashboard (/_/)."
    search-placeholder="Search by email or name..."
    :loading="loading"
    :error="error"
    :is-empty="operators.length === 0"
    :has-query="!!searchQuery"
    empty-icon="👥"
    empty-title="No operators yet"
    empty-message="Add the people who manage this system, each with a set of permissions."
    error-title="Failed to load operators"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/operators/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Operator</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/operators/new" class="btn btn-primary">Create Operator</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="operators" :columns="columns" :loading="loading" @row-click="(u) => router.push(`/operators/${u.id}/edit`)">
        <template #cell-email="{ item }"><div class="font-medium">{{ item.email }}</div></template>
        <template #card-email="{ item }"><div class="text-sm font-bold text-primary truncate">{{ item.email }}</div></template>

        <template #cell-permissions="{ item }">
          <div class="flex flex-wrap items-center gap-1">
            <span class="badge badge-sm" :class="(item.permissions?.length ?? 0) ? 'badge-primary' : 'badge-ghost'">{{ presetLabel(item.permissions || []) }}</span>
            <span v-for="c in item.permissions || []" :key="c" class="badge badge-ghost badge-sm">{{ c }}</span>
          </div>
        </template>
        <template #card-permissions="{ item }">
          <div class="flex flex-wrap items-center gap-1">
            <span class="badge badge-sm" :class="(item.permissions?.length ?? 0) ? 'badge-primary' : 'badge-ghost'">{{ presetLabel(item.permissions || []) }}</span>
            <span v-for="c in item.permissions || []" :key="c" class="badge badge-ghost badge-sm">{{ c }}</span>
          </div>
        </template>

        <template #cell-verified="{ item }">
          <span class="badge badge-sm" :class="item.verified ? 'badge-success' : 'badge-ghost'">{{ item.verified ? 'yes' : 'no' }}</span>
        </template>
        <template #card-verified="{ item }">
          <span class="badge badge-sm" :class="item.verified ? 'badge-success' : 'badge-ghost'">{{ item.verified ? 'yes' : 'no' }}</span>
        </template>

        <template #cell-notify="{ item }">
          <span class="badge badge-sm" :class="item.notify ? 'badge-warning' : 'badge-ghost'">{{ notifyLabel(item) }}</span>
        </template>
        <template #card-notify="{ item }">
          <span class="badge badge-sm" :class="item.notify ? 'badge-warning' : 'badge-ghost'">{{ notifyLabel(item) }}</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/operators/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ operators.length }} of {{ totalItems }} operator(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
