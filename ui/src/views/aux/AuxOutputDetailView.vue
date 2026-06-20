<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { AuxOutput, PointStatus } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<AuxOutput | null>(null)
const status = ref<PointStatus | null>(null)
const loading = ref(true)
const deleting = ref(false)
const commanding = ref(false)
let unsubStatus: (() => void) | null = null

const title = computed(() => record.value?.name || record.value?.code || 'Aux Output')
const kvKey = computed(() => (record.value ? policyKey('aux_output', record.value) : ''))
const statusKey = computed(() => (record.value ? `auxout.${record.value.code}` : ''))

function changedAt(): string {
  if (!status.value?.changed) return '—'
  const d = new Date(status.value.changed)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('aux_output').getOne<AuxOutput>(recordId, { expand: 'location,controller' })
    await loadStatus()
    await subscribeStatus()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load aux output')
    router.push('/aux-outputs')
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

async function drive(action: 'on' | 'off' | 'pulse') {
  commanding.value = true
  try {
    await pb.send(`/api/aux-outputs/${recordId}/output`, { method: 'POST', body: { action } })
    toast.success(`Output: ${action}`)
  } catch (err: any) {
    toast.error(err?.message || 'Failed to drive output')
  } finally {
    commanding.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Aux Output',
    message: `Delete aux output "${record.value.code}"?`,
    details: 'The controller will stop driving this relay. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('aux_output').delete(recordId)
    toast.success('Aux output deleted')
    router.push('/aux-outputs')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete aux output')
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
    :breadcrumbs="[{ label: 'Aux Outputs', to: '/aux-outputs' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/aux-outputs/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Live status">
      <div v-if="status" class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Relay">
          <span class="badge badge-sm" :class="status.state === 'energized' ? 'badge-success' : 'badge-ghost'">
            {{ status.state === 'energized' ? 'Energized' : 'Off' }}
          </span>
        </DataField>
        <DataField label="Updated">{{ changedAt() }}</DataField>
      </div>
      <p v-else class="text-sm opacity-50">
        No live status yet — the controller hasn’t reported (offline or unassigned).
      </p>
    </BaseCard>

    <BaseCard title="Controls">
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-sm btn-outline btn-success" :disabled="commanding" @click="drive('on')">On</button>
        <button class="btn btn-sm btn-outline" :disabled="commanding" @click="drive('off')">Off</button>
        <button class="btn btn-sm btn-primary" :disabled="commanding" @click="drive('pulse')">
          Pulse ({{ record.pulse_seconds }}s)
        </button>
      </div>
      <p class="text-xs opacity-50 mt-2">
        On/Off set the standing state; Pulse energizes momentarily. The live status reflects the standing state.
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
        <DataField label="Relay index">{{ record.relay_index }}</DataField>
        <DataField label="Pulse">{{ record.pulse_seconds }} s</DataField>
      </div>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
