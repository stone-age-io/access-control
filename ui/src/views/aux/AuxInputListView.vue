<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import type { AuxInput } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<AuxInput>('aux_input', 50)
const searchQuery = ref('')
const deleting = ref(false)

function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const filter = q ? `code ~ "${q}" || name ~ "${q}"` : ''
  return { sort: 'code', filter, expand: 'location,controller' }
}

function reload() {
  page.value = 1
  load(queryOpts())
}

const columns: Column<AuxInput>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'controller', label: 'Controller' },
  { key: 'input_index', label: 'Input #' },
]

async function handleDelete(a: AuxInput) {
  const confirmed = await confirm({
    title: 'Delete Aux Input',
    message: `Delete aux input "${a.code}"?`,
    details: 'The controller will stop monitoring this input. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('aux_input').delete(a.id)
    toast.success('Aux input deleted')
    reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete aux input')
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
    title="Aux Inputs"
    subtitle="Named observe-only digital inputs on a controller — surfaced live, no door semantics."
    search-placeholder="Search by code or name..."
    :loading="loading"
    :error="error"
    :is-empty="items.length === 0"
    :has-query="!!searchQuery"
    empty-icon="🔌"
    empty-title="No aux inputs yet"
    empty-message="Add an aux input to monitor a digital line."
    error-title="Failed to load aux inputs"
    @retry="reload"
  >
    <template #actions>
      <router-link to="/aux-inputs/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Aux Input</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link to="/aux-inputs/new" class="btn btn-primary">Create Aux Input</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="items" :columns="columns" :loading="loading" @row-click="(a) => router.push(`/aux-inputs/${a.id}`)">
        <template #cell-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold">{{ item.code }}</code></template>

        <template #cell-name="{ item }">{{ item.name || '—' }}</template>
        <template #card-name="{ item }">{{ item.name || '—' }}</template>

        <template #cell-controller="{ item }"><code class="text-xs">{{ item.expand?.controller?.code || '—' }}</code></template>
        <template #card-controller="{ item }"><code class="text-xs">{{ item.expand?.controller?.code || '—' }}</code></template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">No matches<template v-if="searchQuery"> for “{{ searchQuery }}”</template>.</span>
          </div>
        </template>

        <template #actions="{ item }">
          <router-link :to="`/aux-inputs/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="prevPage(queryOpts())" @next="nextPage(queryOpts())">
        {{ items.length }} of {{ totalItems }} aux input(s)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>
