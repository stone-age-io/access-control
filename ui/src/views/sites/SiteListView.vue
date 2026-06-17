<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { pb } from '@/utils/pb'
import { formatDate } from '@/utils/format'
import type { Site } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()

const { items: sites, totalItems, loading, error, load } = usePagination<Site>('sites', 50)
const searchQuery = ref('')
const deleting = ref(false)

const filtered = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return sites.value
  return sites.value.filter(s =>
    s.code?.toLowerCase().includes(q) ||
    s.name?.toLowerCase().includes(q) ||
    s.timezone?.toLowerCase().includes(q)
  )
})

const columns: Column<Site>[] = [
  { key: 'code', label: 'Code' },
  { key: 'name', label: 'Name' },
  { key: 'timezone', label: 'Timezone' },
  { key: 'fai_suppress', label: 'FAI Suppress' },
  { key: 'created', label: 'Created', format: (v) => formatDate(v, 'PP') },
]

function loadSites() {
  load({ sort: 'code' })
}

async function handleDelete(site: Site) {
  const confirmed = await confirm({
    title: 'Delete Site',
    message: `Delete site "${site.code}"?`,
    details: 'Access points referencing this site will be left without one. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('sites').delete(site.id)
    toast.success('Site deleted')
    loadSites()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete site')
  } finally {
    deleting.value = false
  }
}

onMounted(loadSites)
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Sites</h1>
        <p class="text-base-content/70 mt-1">Buildings/campuses — each owns a timezone.</p>
      </div>
      <router-link to="/sites/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Site</span>
      </router-link>
    </div>

    <div class="form-control">
      <input v-model="searchQuery" type="text" placeholder="Search by code, name, or timezone..." class="input input-bordered w-full" />
    </div>

    <div v-if="loading && sites.length === 0" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <BaseCard v-else-if="error && sites.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">&#9888;</span>
        <h3 class="text-xl font-bold mt-4">Failed to load sites</h3>
        <p class="text-base-content/70 mt-2">{{ error }}</p>
        <button @click="loadSites" class="btn btn-primary mt-4">Retry</button>
      </div>
    </BaseCard>

    <BaseCard v-else-if="sites.length === 0">
      <div class="text-center py-12">
        <span class="text-6xl">🏢</span>
        <h3 class="text-xl font-bold mt-4">No sites yet</h3>
        <p class="text-base-content/70 mt-2">Create your first site to anchor access points and timezones.</p>
        <router-link to="/sites/new" class="btn btn-primary mt-4">Create Site</router-link>
      </div>
    </BaseCard>

    <BaseCard v-else :no-padding="true">
      <ResponsiveList :items="filtered" :columns="columns" :loading="loading" @row-click="(s) => router.push(`/sites/${s.id}/edit`)">
        <template #cell-code="{ item }"><code class="text-xs font-bold text-primary">{{ item.code }}</code></template>
        <template #card-code="{ item }"><code class="text-sm font-bold text-primary">{{ item.code }}</code></template>

        <template #cell-fai_suppress="{ item }">
          <span class="badge badge-sm" :class="item.fai_suppress ? 'badge-success' : 'badge-ghost'">
            {{ item.fai_suppress ? 'on' : 'off' }}
          </span>
        </template>
        <template #card-fai_suppress="{ item }">
          <span class="badge badge-sm" :class="item.fai_suppress ? 'badge-success' : 'badge-ghost'">
            {{ item.fai_suppress ? 'on' : 'off' }}
          </span>
        </template>

        <template #cell-timezone="{ item }"><span class="font-mono text-xs">{{ item.timezone }}</span></template>

        <template #actions="{ item }">
          <router-link :to="`/sites/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button @click="handleDelete(item)" class="btn btn-xs text-error" :disabled="deleting">Delete</button>
        </template>
      </ResponsiveList>

      <div class="p-4 border-t border-base-300 text-sm text-base-content/60">{{ totalItems }} site(s)</div>
    </BaseCard>
  </div>
</template>
