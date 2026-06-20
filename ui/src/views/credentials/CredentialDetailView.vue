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
import RailCard from '@/components/ui/RailCard.vue'

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

function statusBadge(status: string): string {
  if (status === 'active') return 'badge-success'
  if (status === 'revoked') return 'badge-error'
  return 'badge-warning'
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

    <BaseCard title="Credential">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Value">
          <code class="text-sm">{{ record.value }}</code>
        </DataField>
        <DataField label="Type">
          <span class="badge badge-ghost badge-sm">{{ record.type || '—' }}</span>
        </DataField>
        <DataField label="Status">
          <span class="badge badge-sm" :class="statusBadge(record.status || '')">{{ record.status || 'active' }}</span>
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

    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RailCard v-if="holder" title="Cardholder" icon="🪪">
        <router-link :to="`/cardholders/${holder.id}`" class="flex flex-col gap-0.5 hover:opacity-80">
          <span class="text-sm font-medium">{{ holder.name || holder.email || holder.id }}</span>
          <span v-if="holder.email && holder.name" class="text-xs opacity-50">{{ holder.email }}</span>
        </router-link>
      </RailCard>
    </template>
  </DetailLayout>
</template>
