<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Site, AccessPoint } from '@/types/pocketbase'
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
const record = ref<Site | null>(null)
const accessPoints = ref<AccessPoint[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Site')
const kvKey = computed(() => (record.value ? policyKey('sites', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [s, points] = await Promise.all([
      pb.collection('sites').getOne<Site>(recordId),
      pb.collection('access_points').getFullList<AccessPoint>({ filter: `site = "${recordId}"`, sort: 'code' }),
    ])
    record.value = s
    accessPoints.value = points
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load site')
    router.push('/sites')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Site',
    message: `Delete site "${record.value.code}"?`,
    details: 'Access points referencing this site will be left without one. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('sites').delete(recordId)
    toast.success('Site deleted')
    router.push('/sites')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete site')
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
    :breadcrumbs="[{ label: 'Sites', to: '/sites' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/sites/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Site">
      <div class="grid grid-cols-2 gap-x-6 gap-y-4">
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
        title="Access Points"
        icon="🚪"
        :items="accessPoints"
        :to="(p) => `/access-points/${p.id}`"
        :primary="(p) => p.code"
        :secondary="(p) => p.name"
        empty="No access points in this site."
      />
    </template>
  </DetailLayout>
</template>
