<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { AccessGroup } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: groups, totalItems, loading, error, load } = usePagination<AccessGroup>('access_groups', 50)
const searchQuery = ref('')
const deleting = ref(false)

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return groups.value
  return groups.value.filter(g => g.code?.toLowerCase().includes(q) || g.name?.toLowerCase().includes(q))
})

const columns: Column<AccessGroup>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'expand.schedule.code', label: 'Schedule' },
  { key: 'access_points', label: 'Points', format: (v) => `${(v || []).length}` },
]

function loadGroups() {
  load({ sort: 'code', expand: 'schedule,access_points' })
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
    loadGroups()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete access group')
  } finally {
    deleting.value = false
  }
}

onMounted(loadGroups)
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
        <button @click="loadGroups" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="groups.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">🗝️</span>
        <h3 class="text-xl font-bold mt-4">No access groups yet</h3>
        <p class="text-base-content/70 mt-2">Group access points under a schedule, then assign groups to roles.</p>
        <router-link to="/access-groups/new" class="btn btn-primary mt-4">Create Access Group</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(g) => router.push(`/access-groups/${g.id}/edit`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-expand.schedule.code="{ item }">
          <code v-if="item.expand?.schedule" class="text-xs">{{ item.expand.schedule.code }}</code>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-expand.schedule.code="{ item }">
          <code v-if="item.expand?.schedule" class="text-xs">{{ item.expand.schedule.code }}</code>
          <span v-else>-</span>
        </template>

        <template #cell-access_points="{ item }"><span class="badge badge-ghost badge-sm">{{ (item.access_points || []).length }}</span></template>

        <template #actions="{ item }">
          <router-link :to="`/access-groups/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>
      <div class="p-4 border-t border-base-300 text-sm text-base-content/60">{{ totalItems }} access group(s)</div>
    </BaseCard>
  </div>
</template>
