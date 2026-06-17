<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { Cardholder } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: cardholders, totalItems, loading, error, load } = usePagination<Cardholder>('cardholders', 50)
const searchQuery = ref('')
const deleting = ref(false)

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return cardholders.value
  return cardholders.value.filter(c =>
    c.name?.toLowerCase().includes(q) ||
    c.email?.toLowerCase().includes(q) ||
    c.external_id?.toLowerCase().includes(q)
  )
})

const columns: Column<Cardholder>[] = [
  { key: 'name', label: 'Name' },
  { key: 'email', label: 'Email' },
  { key: 'status', label: 'Status' },
  { key: 'roles', label: 'Roles', format: (v) => `${(v || []).length}` },
  { key: 'external_id', label: 'External ID' },
]

function loadCardholders() {
  load({ sort: 'name', expand: 'roles' })
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
    loadCardholders()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete cardholder')
  } finally {
    deleting.value = false
  }
}

onMounted(loadCardholders)
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
        <button @click="loadCardholders" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="cardholders.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">🪪</span>
        <h3 class="text-xl font-bold mt-4">No cardholders yet</h3>
        <p class="text-base-content/70 mt-2">Add the people who hold credentials, then assign them roles.</p>
        <router-link to="/cardholders/new" class="btn btn-primary mt-4">Create Cardholder</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/cardholders/${c.id}/edit`)">
        <template #cell-name="{ item }"><div class="font-medium">{{ item.name || 'Unnamed' }}</div></template>
        <template #card-name="{ item }"><div class="text-sm font-bold text-primary truncate">{{ item.name || 'Unnamed' }}</div></template>

        <template #cell-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'active' ? 'badge-success' : 'badge-warning'">{{ item.status || 'active' }}</span>
        </template>
        <template #card-status="{ item }">
          <span class="badge badge-sm" :class="item.status === 'active' ? 'badge-success' : 'badge-warning'">{{ item.status || 'active' }}</span>
        </template>

        <template #cell-roles="{ item }"><span class="badge badge-ghost badge-sm">{{ (item.roles || []).length }}</span></template>
        <template #card-roles="{ item }"><span class="badge badge-ghost badge-sm">{{ (item.roles || []).length }}</span></template>

        <template #cell-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else>-</span>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/cardholders/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>
      <div class="p-4 border-t border-base-300 text-sm text-base-content/60">{{ totalItems }} cardholder(s)</div>
    </BaseCard>
  </div>
</template>
