<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import { formatDate } from '@/utils/format'
import type { Credential, Cardholder } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import type { SoftTone } from '@/utils/badges'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Credential | null>(null)
const loading = ref(true)
const deleting = ref(false)

const holder = computed<Cardholder | undefined>(() => record.value?.expand?.user)
const title = computed(() => record.value?.value || 'Credential')
const kvKey = computed(() => (record.value ? policyKey('credentials', record.value) : ''))

function statusTone(status: string): SoftTone {
  if (status === 'active') return 'success'
  if (status === 'revoked') return 'error'
  return 'warning'
}

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('credentials').getOne<Credential>(recordId, { expand: 'user' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load credential')
    router.push('/credentials')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Credential',
    message: `Delete credential "${record.value.value}"?`,
    details: 'It will stop being accepted at readers once the controller syncs. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('credentials').delete(recordId)
    toast.success('Credential deleted')
    router.push('/credentials')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete credential')
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
    :breadcrumbs="[{ label: 'Credentials', to: '/credentials' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/credentials/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard>
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Value">
          <code class="text-sm">{{ record.value }}</code>
        </DataField>
        <DataField label="Type">
          <SoftBadge>{{ record.type || '—' }}</SoftBadge>
        </DataField>
        <DataField label="Status">
          <SoftBadge :tone="statusTone(record.status || '')" dot>{{ record.status || 'active' }}</SoftBadge>
        </DataField>
        <DataField label="Label">{{ record.label || '—' }}</DataField>
        <DataField v-if="record.valid_from" label="Valid from">{{ formatDate(record.valid_from) }}</DataField>
        <DataField v-if="record.valid_until" label="Valid until">{{ formatDate(record.valid_until) }}</DataField>
        <DataField label="Cardholder">
          <router-link v-if="holder" :to="`/cardholders/${holder.id}`" class="link link-primary">
            {{ holder.name || holder.email || holder.id }}
          </router-link>
          <span v-else class="opacity-40">— (no holder)</span>
        </DataField>
      </div>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
