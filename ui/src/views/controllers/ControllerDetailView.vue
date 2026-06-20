<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import { formatDate, formatRelativeTime } from '@/utils/format'
import type { Controller, Portal } from '@/types/pocketbase'
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
const record = ref<Controller | null>(null)
const portals = ref<Portal[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Controller')
const kvKey = computed(() => (record.value ? policyKey('controllers', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [c, p] = await Promise.all([
      pb.collection('controllers').getOne<Controller>(recordId, { expand: 'location' }),
      pb.collection('portals').getFullList<Portal>({ filter: `controller = "${recordId}"`, sort: 'code' }),
    ])
    record.value = c
    portals.value = p
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load controller')
    router.push('/controllers')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Controller',
    message: `Delete controller "${record.value.code}"?`,
    details: 'Portals assigned to it will become unassigned and will not be armed until reassigned. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('controllers').delete(recordId)
    toast.success('Controller deleted')
    router.push('/controllers')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete controller')
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
    :breadcrumbs="[{ label: 'Controllers', to: '/controllers' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/controllers/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Controller">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Location">
          <router-link v-if="record.expand?.location" :to="`/locations/${record.expand.location.id}`" class="link link-primary">
            {{ record.expand.location.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Model">
          <span class="badge badge-ghost badge-sm font-mono">{{ record.model || '—' }}</span>
        </DataField>
        <DataField label="Status">
          <span class="badge badge-sm" :class="record.status === 'online' ? 'badge-success' : 'badge-ghost'">{{ record.status || 'unknown' }}</span>
        </DataField>
        <DataField label="Last seen">
          <span v-if="record.last_seen" :title="formatDate(record.last_seen)">{{ formatRelativeTime(record.last_seen) }}</span>
          <span v-else class="opacity-40">never</span>
        </DataField>
      </div>
    </BaseCard>

    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="Drives portals"
        icon="🚪"
        :items="portals"
        :to="(p) => `/portals/${p.id}`"
        :primary="(p) => p.code"
        :secondary="(p) => p.name"
        empty="No portals assigned to this controller yet."
      />
    </template>
  </DetailLayout>
</template>
