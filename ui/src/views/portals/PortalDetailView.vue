<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Portal, AccessGroup } from '@/types/pocketbase'
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
const record = ref<Portal | null>(null)
const groups = ref<AccessGroup[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Portal')
const kvKey = computed(() => (record.value ? policyKey('portals', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [p, g] = await Promise.all([
      pb.collection('portals').getOne<Portal>(recordId, { expand: 'location,controller,auto_schedule' }),
      pb.collection('access_groups').getFullList<AccessGroup>({ filter: `portals ~ "${recordId}"`, sort: 'code' }),
    ])
    record.value = p
    groups.value = g
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load portal')
    router.push('/portals')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Portal',
    message: `Delete portal "${record.value.code}"?`,
    details: 'Access groups referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('portals').delete(recordId)
    toast.success('Portal deleted')
    router.push('/portals')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete portal')
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
    :breadcrumbs="[{ label: 'Portals', to: '/portals' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/portals/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Portal">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Type">
          <span class="badge badge-ghost badge-sm">{{ record.type || '—' }}</span>
        </DataField>
        <DataField label="Location">
          <router-link v-if="record.expand?.location" :to="`/locations/${record.expand.location.id}`" class="link link-primary">
            {{ record.expand.location.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Standing Posture">
          <span class="badge badge-sm badge-ghost">{{ record.posture || 'secure' }}</span>
        </DataField>
        <DataField label="Pulse">{{ record.pulse_seconds }} s</DataField>
      </div>
    </BaseCard>

    <BaseCard title="Controller &amp; hardware">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Controller">
          <router-link v-if="record.expand?.controller" :to="`/controllers/${record.expand.controller.id}`" class="link link-primary">
            {{ record.expand.controller.code }}
          </router-link>
          <span v-else class="opacity-40">Unassigned</span>
        </DataField>
        <DataField label="Held-open">{{ record.held_open_seconds }} s</DataField>
        <DataField label="Lock relay">{{ record.lock_relay }}</DataField>
        <DataField label="DPS input">{{ record.dps_input }}</DataField>
        <DataField label="REX input">{{ record.rex_input }}</DataField>
      </div>
    </BaseCard>

    <BaseCard title="Scheduled posture">
      <div v-if="!record.auto_posture && !record.auto_schedule" class="text-sm opacity-50">
        No scheduled posture — the standing posture always applies.
      </div>
      <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Posture">
          <span class="badge badge-sm badge-ghost">{{ record.auto_posture || '—' }}</span>
        </DataField>
        <DataField label="Schedule">
          <router-link v-if="record.expand?.auto_schedule" :to="`/schedules/${record.expand.auto_schedule.id}`" class="link link-primary">
            {{ record.expand.auto_schedule.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
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
