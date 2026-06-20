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
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

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
  <ListLayout
    v-model:search="searchQuery"
    title="Credentials"
    subtitle="Opaque strings presented at a reader — each mapped to one cardholder."
    search-placeholder="Search by value, label, or cardholder..."
    :loading="loading"
    :error="error"
    :is-empty="credentials.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🎫"
    empty-title="No credentials yet"
    empty-message="Issue a credential to a cardholder so they can present it at a reader."
    error-title="Failed to load credentials"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/credentials/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Credential</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/credentials/new" class="btn btn-primary">Create Credential</router-link>
    </template>

    <BaseCard :no-padding="true">
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

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ credentials.length }} of {{ totalItems }} credential(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
