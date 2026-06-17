<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { Credential } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: credentials, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Credential>('credentials', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `value ~ "${q}" || label ~ "${q}" || user.name ~ "${q}" || user.email ~ "${q}"` : ''
  return { sort: 'value', expand: 'user', filter }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<Credential>[] = [
  { key: 'value', label: 'Value' },
  { key: 'type', label: 'Type' },
  { key: 'expand.user.name', label: 'Cardholder' },
  { key: 'status', label: 'Status' },
  { key: 'label', label: 'Label' },
]

const STATUS_BADGE: Record<string, string> = {
  active: 'badge-success',
  revoked: 'badge-error',
  suspended: 'badge-warning',
}

async function handleDelete(c: Credential) {
  const confirmed = await confirm({
    title: 'Delete Credential',
    message: `Delete credential "${c.value}"?`,
    details: 'Revoking instead of deleting preserves the audit trail. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('credentials').delete(c.id)
    toast.success('Credential deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete credential')
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
        <h1 class="text-3xl font-bold">Credentials</h1>
        <p class="text-base-content/70 mt-1">Opaque strings presented at a reader — each mapped to one cardholder.</p>
      </div>
      <router-link to="/credentials/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Credential</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by value, label, or cardholder..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && credentials.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && credentials.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load credentials</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="reload" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="credentials.length === 0 && !searchQuery">
      <div class="text-center py-12">
        <span class="text-6xl">🎫</span>
        <h3 class="text-xl font-bold mt-4">No credentials yet</h3>
        <p class="text-base-content/70 mt-2">Issue a credential to a cardholder so they can present it at a reader.</p>
        <router-link to="/credentials/new" class="btn btn-primary mt-4">Create Credential</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="credentials" :columns="columns" :loading="loading" @row-click="(c) => router.push(`/credentials/${c.id}`)">
        <template #cell-value="{ item }"><code class="text-xs font-bold text-primary">{{ item.value }}</code></template>
        <template #card-value="{ item }"><code class="text-sm font-bold text-primary">{{ item.value }}</code></template>

        <template #cell-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>
        <template #card-type="{ item }">
          <span class="badge badge-ghost badge-sm">{{ item.type || '-' }}</span>
        </template>

        <template #cell-expand.user.name="{ item }">
          <router-link v-if="item.expand?.user" :to="`/cardholders/${item.expand.user.id}`" class="link link-hover" @click.stop>
            {{ item.expand.user.name || item.expand.user.email }}
          </router-link>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-expand.user.name="{ item }">
          <span v-if="item.expand?.user">{{ item.expand.user.name || item.expand.user.email }}</span>
          <span v-else class="text-base-content/40">-</span>
        </template>

        <template #cell-status="{ item }">
          <span class="badge badge-sm" :class="STATUS_BADGE[item.status] || 'badge-ghost'">{{ item.status || '-' }}</span>
        </template>
        <template #card-status="{ item }">
          <span class="badge badge-sm" :class="STATUS_BADGE[item.status] || 'badge-ghost'">{{ item.status || '-' }}</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 opacity-40">
            <span class="text-4xl">🔍</span>
            <span class="text-sm font-bold uppercase tracking-widest">No matches</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/credentials/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
        <span class="text-sm text-base-content/60">{{ credentials.length }} of {{ totalItems }} credential(s)</span>
        <div v-if="totalPages > 1" class="join">
          <button class="join-item btn btn-sm" :disabled="page === 1 || loading" @click="prevPage(queryOpts())">«</button>
          <button class="join-item btn btn-sm">{{ page }} / {{ totalPages }}</button>
          <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" @click="nextPage(queryOpts())">»</button>
        </div>
      </div>
    </BaseCard>
  </div>
</template>
