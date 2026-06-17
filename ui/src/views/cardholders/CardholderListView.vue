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
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Cardholders</h1>
        <p class="text-base-content/70 mt-1">People who hold credentials (not PocketBase logins).</p>
      </div>
      <router-link to="/cardholders/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Cardholder</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by name, email, or external ID..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && cardholders.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && cardholders.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load cardholders</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="cardholders.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🪪</span>
        <h3 class="text-xl font-bold mt-4">No cardholders yet</h3>
        <p class="text-base-content/70 mt-2">Add the people who hold credentials, then assign them roles.</p>
        <router-link to="/cardholders/new" class="btn btn-primary mt-4">Create Cardholder</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="cardholders" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/cardholders/${c.id}`)">
        <template #cell-name="{ item }"><div class="font-medium">{{ item.name || 'Unnamed' }}</div></template>
        <template #card-name="{ item }"><div class="text-sm font-bold text-primary truncate">{{ item.name || 'Unnamed' }}</div></template>

        <template #cell-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'active' ? 'badge-success' : 'badge-warning'">{{ item.status || 'active' }}</span>
        </template>
        <template #card-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'active' ? 'badge-success' : 'badge-warning'">{{ item.status || 'active' }}</span>
        </template>

        <template #cell-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1">
            <code v-for="r in rolesOf(item).slice(0, 3)" :key="r.id" class="badge badge-ghost badge-sm">{{ r.code }}</code>
            <span v-if="rolesOf(item).length > 3" class="badge badge-ghost badge-sm">+{{ rolesOf(item).length - 3 }}</span>
          </div>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <code v-for="r in rolesOf(item).slice(0, 2)" :key="r.id" class="badge badge-ghost badge-sm">{{ r.code }}</code>
            <span v-if="rolesOf(item).length > 2" class="badge badge-ghost badge-sm">+{{ rolesOf(item).length - 2 }}</span>
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
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/cardholders/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ cardholders.length }} of {{ totalItems }} cardholder(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
