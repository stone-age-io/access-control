<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { AccessPoint, AccessGroup } from '@/types/pocketbase'
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
const record = ref<AccessPoint | null>(null)
const groups = ref<AccessGroup[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Access Point')
const kvKey = computed(() => (record.value ? policyKey('access_points', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [p, g] = await Promise.all([
      pb.collection('access_points').getOne<AccessPoint>(recordId, { expand: 'site' }),
      pb.collection('access_groups').getFullList<AccessGroup>({ filter: `access_points ~ "${recordId}"`, sort: 'code' }),
    ])
    record.value = p
    groups.value = g
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load access point')
    router.push('/access-points')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Access Point',
    message: `Delete access point "${record.value.code}"?`,
    details: 'Access groups referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('access_points').delete(recordId)
    toast.success('Access point deleted')
    router.push('/access-points')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete access point')
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
    :breadcrumbs="[{ label: 'Access Points', to: '/access-points' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/access-points/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Access Point">
      <div class="grid grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Site">
          <router-link v-if="record.expand?.site" :to="`/sites/${record.expand.site.id}`" class="link link-primary">
            {{ record.expand.site.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Standing Posture">
          <span class="badge badge-sm badge-ghost">{{ record.posture || 'secure' }}</span>
        </DataField>
        <DataField label="Pulse">{{ record.pulse_seconds }} s</DataField>
      </div>
    </BaseCard>

    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="In access groups"
        icon="🗝️"
        :items="groups"
        :to="(g) => `/access-groups/${g.id}`"
        :primary="(g) => g.code"
        :secondary="(g) => g.name"
        empty="Not in any access group yet."
      />
    </template>
  </DetailLayout>
</template>
