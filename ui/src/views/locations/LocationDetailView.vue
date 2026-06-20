<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Location, Portal } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RefList from '@/components/ui/RefList.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Location | null>(null)
const portals = ref<Portal[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Location')
const kvKey = computed(() => (record.value ? policyKey('locations', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [l, pts] = await Promise.all([
      pb.collection('locations').getOne<Location>(recordId),
      pb.collection('portals').getFullList<Portal>({ filter: `location = "${recordId}"`, sort: 'code' }),
    ])
    record.value = l
    portals.value = pts
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load location')
    router.push('/locations')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Location',
    message: `Delete location "${record.value.code}"?`,
    details: 'Portals referencing this location will be left without one. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('locations').delete(recordId)
    toast.success('Location deleted')
    router.push('/locations')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete location')
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <DetailLayout
    v-else-if="record"
    :title="title"
    :breadcrumbs="[{ label: 'Locations', to: '/locations' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/locations/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Location">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Timezone">
          <code class="text-sm">{{ record.timezone }}</code>
        </DataField>
        <DataField label="FAI Suppress">
          <span class="badge badge-sm" :class="record.fai_suppress ? 'badge-success' : 'badge-ghost'">
            {{ record.fai_suppress ? 'suppressed' : 'off' }}
          </span>
        </DataField>
      </div>
    </BaseCard>

    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="Portals"
        icon="🚪"
        :items="portals"
        :to="(p) => `/portals/${p.id}`"
        :primary="(p) => p.code"
        :secondary="(p) => p.name"
        empty="No portals in this location."
      />
    </template>
  </DetailLayout>
</template>
