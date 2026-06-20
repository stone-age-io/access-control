<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { AccessGroup, Portal } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

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
  return { sort: 'code', expand: 'portals,schedule', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<AccessGroup>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'expand.schedule.code', label: 'Schedule' },
  { key: 'portals', label: 'Portals' },
]

function portalsOf(g: AccessGroup): Portal[] {
  return g.expand?.portals || []
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
  <ListLayout
    v-model:search="searchQuery"
    title="Access Groups"
    subtitle="A set of portals opened under one schedule (an &quot;access level&quot;)."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="groups.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🗝️"
    empty-title="No access groups yet"
    empty-message="Group portals under a schedule, then assign groups to roles."
    error-title="Failed to load access groups"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/access-groups/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Access Group</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/access-groups/new" class="btn btn-primary">Create Access Group</router-link>
    </template>

    <BaseCard :no-padding="true">
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

        <template #cell-portals="{ item }">
          <div v-if="portalsOf(item).length" class="flex flex-wrap gap-1">
            <code v-for="p in portalsOf(item).slice(0, 3)" :key="p.id" class="badge badge-ghost badge-sm">{{ p.code }}</code>
            <span v-if="portalsOf(item).length > 3" class="badge badge-ghost badge-sm">+{{ portalsOf(item).length - 3 }}</span>
          </div>
          <span v-else-if="(item.portals || []).length" class="badge badge-ghost badge-sm">{{ (item.portals || []).length }}</span>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-portals="{ item }">
          <div v-if="portalsOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <code v-for="p in portalsOf(item).slice(0, 2)" :key="p.id" class="badge badge-ghost badge-sm">{{ p.code }}</code>
            <span v-if="portalsOf(item).length > 2" class="badge badge-ghost badge-sm">+{{ portalsOf(item).length - 2 }}</span>
          </div>
          <span v-else-if="(item.portals || []).length" class="badge badge-ghost badge-sm">{{ (item.portals || []).length }}</span>
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

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ groups.length }} of {{ totalItems }} access group(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
