<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import { formatDate, formatRelativeTime } from '@/utils/format'
import { modelProfile, type ModelProfile } from '@/utils/models'
import { buildControllerIO, type ControllerIO } from '@/utils/io'
import type { Controller, Portal, AuxInput, AuxOutput } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'
import ControllerIOMap from '@/components/ui/ControllerIOMap.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Controller | null>(null)
const portals = ref<Portal[]>([])
const auxInputs = ref<AuxInput[]>([])
const auxOutputs = ref<AuxOutput[]>([])
const profile = ref<ModelProfile | null>(null)
const io = ref<ControllerIO>({ relays: new Map(), inputs: new Map() })
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Controller')
const kvKey = computed(() => (record.value ? policyKey('controllers', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [c, p, ai, ao] = await Promise.all([
      pb.collection('controllers').getOne<Controller>(recordId, { expand: 'location' }),
      pb.collection('portals').getFullList<Portal>({ filter: `controller = "${recordId}"`, sort: 'code' }),
      pb.collection('aux_input').getFullList<AuxInput>({ filter: `controller = "${recordId}"`, sort: 'code' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ filter: `controller = "${recordId}"`, sort: 'code' }),
    ])
    record.value = c
    portals.value = p
    auxInputs.value = ai
    auxOutputs.value = ao
    profile.value = await modelProfile(c.model || '')
    io.value = buildControllerIO(p, ai, ao)
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

    <BaseCard>
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
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

    <ControllerIOMap :profile="profile" :io="io" />

    <RelationList
      title="Drives portals"
      icon="🚪"
      :items="portals"
      :to="(p) => `/portals/${p.id}`"
      :primary="(p) => p.code"
      :secondary="(p) => p.name"
      empty="No portals assigned to this controller yet."
    >
      <template #actions>
        <router-link :to="`/portals/new?controller=${record.id}`" class="btn btn-sm btn-outline">+ Add portal</router-link>
      </template>
    </RelationList>

    <RelationList
      title="Aux inputs"
      icon="🔌"
      :items="auxInputs"
      :to="(a) => `/aux-inputs/${a.id}`"
      :primary="(a) => a.code"
      :secondary="(a) => (a.input_index ? `input ${a.input_index}` : a.name)"
      empty="No aux inputs monitored by this controller."
    >
      <template #actions>
        <router-link :to="`/aux-inputs/new?controller=${record.id}`" class="btn btn-sm btn-outline">+ Add aux input</router-link>
      </template>
    </RelationList>

    <RelationList
      title="Aux outputs"
      icon="⚡"
      :items="auxOutputs"
      :to="(a) => `/aux-outputs/${a.id}`"
      :primary="(a) => a.code"
      :secondary="(a) => (a.relay_index ? `relay ${a.relay_index}` : a.name)"
      empty="No aux outputs driven by this controller."
    >
      <template #actions>
        <router-link :to="`/aux-outputs/new?controller=${record.id}`" class="btn btn-sm btn-outline">+ Add aux output</router-link>
      </template>
    </RelationList>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
