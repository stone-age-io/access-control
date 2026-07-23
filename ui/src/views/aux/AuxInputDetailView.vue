<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { AuxInput, PointStatus } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<AuxInput | null>(null)
const status = ref<PointStatus | null>(null)
const loading = ref(true)
const deleting = ref(false)
let unsubStatus: (() => void) | null = null

const title = computed(() => record.value?.name || record.value?.code || 'Aux Input')
const kvKey = computed(() => (record.value ? policyKey('aux_input', record.value) : ''))
const statusKey = computed(() => (record.value ? `auxin.${record.value.code}` : ''))

function changedAt(): string {
  if (!status.value?.changed) return '—'
  const d = new Date(status.value.changed)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('aux_input').getOne<AuxInput>(recordId, { expand: 'location,controller,area' })
    await loadStatus()
    await subscribeStatus()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load aux input')
    router.push('/aux-inputs')
  } finally {
    loading.value = false
  }
}

async function loadStatus() {
  try {
    status.value = await pb.collection('point_status').getFirstListItem<PointStatus>(`key = "${statusKey.value}"`)
  } catch {
    status.value = null
  }
}

async function subscribeStatus() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    if (e.record.key !== statusKey.value) return
    status.value = e.action === 'delete' ? null : e.record
  })
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Aux Input',
    message: `Delete aux input "${record.value.code}"?`,
    details: 'The controller will stop monitoring this input. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('aux_input').delete(recordId)
    toast.success('Aux input deleted')
    router.push('/aux-inputs')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete aux input')
  } finally {
    deleting.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => {
  if (unsubStatus) unsubStatus()
})
</script>

<template>
  <div v-if="loading" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <DetailLayout
    v-else-if="record"
    :title="title"
    :breadcrumbs="[{ label: 'Aux Inputs', to: '/aux-inputs' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/aux-inputs/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Live status">
      <div v-if="status" class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Input">
          <SoftBadge :tone="status.state === 'active' ? 'warning' : 'neutral'" dot>
            {{ status.state === 'active' ? 'Active' : 'Inactive' }}
          </SoftBadge>
        </DataField>
        <DataField label="Updated">{{ changedAt() }}</DataField>
      </div>
      <p v-else class="text-sm opacity-50">
        No live status yet — the controller hasn’t reported (offline or unassigned).
      </p>
    </BaseCard>

    <BaseCard title="Identity">
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Code"><code class="text-sm">{{ record.code }}</code></DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Location">
          <router-link v-if="record.expand?.location" :to="`/locations/${record.expand.location.id}`" class="link link-primary">
            {{ record.expand.location.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Controller">
          <router-link v-if="record.expand?.controller" :to="`/controllers/${record.expand.controller.id}`" class="link link-primary">
            {{ record.expand.controller.code }}
          </router-link>
          <span v-else class="opacity-40">Unassigned</span>
        </DataField>
        <DataField label="Input index">{{ record.input_index }}</DataField>
        <DataField label="Contact">
          {{ record.contact === 'nc' ? 'Normally closed' : 'Normally open' }}
        </DataField>
        <DataField label="Area">
          <router-link v-if="record.expand?.area" :to="`/areas/${record.expand.area.id}`" class="link link-primary">
            {{ record.expand.area.code }}
          </router-link>
          <span v-else class="opacity-40">None</span>
        </DataField>
        <DataField label="Point type">
          <SoftBadge :tone="record.point_type === 'intrusion' ? 'error' : record.point_type === 'tamper_24h' ? 'warning' : 'neutral'" dot>
            {{ record.point_type || 'monitor' }}
          </SoftBadge>
        </DataField>
      </div>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
