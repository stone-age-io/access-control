<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { Cardholder, Role } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import Avatar from '@/components/ui/Avatar.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: cardholders, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Cardholder>('cardholders', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `name ~ "${q}" || email ~ "${q}" || external_id ~ "${q}"` : ''
  return { sort: 'name', expand: 'roles', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Cardholder>[] = [
  { key: 'name', label: 'Name' },
  { key: 'email', label: 'Email' },
  { key: 'status', label: 'Status' },
  { key: 'roles', label: 'Roles' },
  { key: 'external_id', label: 'External ID' },
]

function rolesOf(c: Cardholder): Role[] {
  return c.expand?.roles || []
}

async function handleDelete(c: Cardholder) {
  const confirmed = await confirm({
    title: 'Delete Cardholder',
    message: `Delete cardholder "${c.name || c.email}"?`,
    details: 'Their credentials will be left without a holder. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('cardholders').delete(c.id)
    toast.success('Cardholder deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete cardholder')
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
    title="Cardholders"
    subtitle="People who hold credentials (not PocketBase logins)."
    search-placeholder="Search by name, email, or external ID..."
    :loading="loading"
    :error="error"
    :is-empty="cardholders.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🪪"
    empty-title="No cardholders yet"
    empty-message="Add the people who hold credentials, then assign them roles."
    error-title="Failed to load cardholders"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/cardholders/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Cardholder</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/cardholders/new" class="btn btn-primary">Create Cardholder</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="cardholders" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/cardholders/${c.id}`)">
        <template #cell-name="{ item }">
          <div class="flex items-center gap-2.5">
            <Avatar :name="item.name" :seed="item.id" />
            <span class="font-medium">{{ item.name || 'Unnamed' }}</span>
          </div>
        </template>
        <template #card-name="{ item }">
          <div class="flex items-center gap-2 min-w-0">
            <Avatar :name="item.name" :seed="item.id" size="xs" />
            <span class="text-sm font-bold text-primary truncate">{{ item.name || 'Unnamed' }}</span>
          </div>
        </template>

        <template #cell-status="{ item }">
          <SoftBadge :tone="item.status === 'active' ? 'success' : 'warning'" dot>{{ item.status || 'active' }}</SoftBadge>
        </template>
        <template #card-status="{ item }">
          <SoftBadge :tone="item.status === 'active' ? 'success' : 'warning'" dot>{{ item.status || 'active' }}</SoftBadge>
        </template>

        <template #cell-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1">
            <SoftBadge v-for="r in rolesOf(item).slice(0, 3)" :key="r.id" class="font-mono">{{ r.code }}</SoftBadge>
            <SoftBadge v-if="rolesOf(item).length > 3">+{{ rolesOf(item).length - 3 }}</SoftBadge>
          </div>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <SoftBadge v-for="r in rolesOf(item).slice(0, 2)" :key="r.id" class="font-mono">{{ r.code }}</SoftBadge>
            <SoftBadge v-if="rolesOf(item).length > 2">+{{ rolesOf(item).length - 2 }}</SoftBadge>
          </div>
          <span v-else>-</span>
        </template>

        <template #cell-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else>-</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">No matches<template v-if="searchQuery"> for “{{ searchQuery }}”</template>.</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/cardholders/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ cardholders.length }} of {{ totalItems }} cardholder(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
