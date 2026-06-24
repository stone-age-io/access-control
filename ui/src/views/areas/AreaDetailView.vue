<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useAuthStore } from '@/stores/auth'
import { useAreaCommands } from '@/composables/useAreaCommands'
import { policyKey } from '@/utils/policyKey'
import { aggregateArm, armBadge, armLabel } from '@/utils/arming'
import type { Area, AuxInput, Portal, PointStatus } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()
const { commanding, arm, disarm, armClear } = useAreaCommands()

const recordId = route.params.id as string
const record = ref<Area | null>(null)
const members = ref<AuxInput[]>([])
const memberPortals = ref<Portal[]>([])
const shadows = ref<PointStatus[]>([]) // per-controller arm shadows for this area
const loading = ref(true)
const deleting = ref(false)
let unsubStatus: (() => void) | null = null

const canCommand = computed(() => auth.can('command'))
const title = computed(() => record.value?.name || record.value?.code || 'Area')
const kvKey = computed(() => (record.value ? policyKey('areas', record.value) : ''))
const agg = computed(() => aggregateArm(shadows.value))

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('areas').getOne<Area>(recordId, { expand: 'location,auto_schedule' })
    members.value = await pb.collection('aux_input').getFullList<AuxInput>({
      filter: `area = "${recordId}"`,
      sort: 'code',
      expand: 'controller',
    })
    memberPortals.value = await pb.collection('portals').getFullList<Portal>({
      filter: `area = "${recordId}"`,
      sort: 'code',
      expand: 'controller',
    })
    await loadShadows()
    await subscribeStatus()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load area')
    router.push('/areas')
  } finally {
    loading.value = false
  }
}

// The per-controller arm shadows share the area code across kind=area rows.
async function loadShadows() {
  if (!record.value) return
  try {
    shadows.value = await pb.collection('point_status').getFullList<PointStatus>({
      filter: `kind = "area" && code = "${record.value.code}"`,
    })
  } catch {
    shadows.value = []
  }
}

async function subscribeStatus() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    if (e.record.kind !== 'area' || e.record.code !== record.value?.code) return
    const m = shadows.value.filter((s) => s.key !== e.record.key)
    if (e.action !== 'delete') m.push(e.record)
    shadows.value = m
  })
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Area',
    message: `Delete area "${record.value.code}"?`,
    details: 'Member inputs keep their wiring but stop arming. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('areas').delete(recordId)
    toast.success('Area deleted')
    router.push('/areas')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete area')
  } finally {
    deleting.value = false
  }
}

function pointTypeBadge(t: string): string {
  if (t === 'intrusion') return 'badge-error'
  if (t === 'tamper_24h') return 'badge-warning'
  return 'badge-ghost'
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
    :breadcrumbs="[{ label: 'Areas', to: '/areas' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/areas/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Arm state">
      <div class="flex flex-wrap items-center gap-3">
        <span class="badge badge-lg" :class="armBadge(agg.state)">{{ armLabel(agg) }}</span>
        <span v-if="record.arm_override" class="badge badge-warning gap-1">
          override: {{ record.arm_override }}
        </span>
        <span v-else class="text-sm opacity-60">standing: {{ record.arm || 'disarmed' }}</span>
        <span v-if="shadows.length === 0" class="text-sm opacity-50">— no controller has reported yet</span>
      </div>

      <div v-if="canCommand" class="flex flex-wrap gap-2 mt-4">
        <button class="btn btn-sm btn-warning" :disabled="commanding" @click="arm(record.id, record.code)">Arm</button>
        <button class="btn btn-sm" :disabled="commanding" @click="disarm(record.id, record.code)">Disarm</button>
        <button class="btn btn-sm btn-ghost" :disabled="commanding || !record.arm_override" @click="armClear(record.id)">Clear override</button>
      </div>
      <p v-else class="text-sm opacity-50 mt-3">You need the <code>command</code> capability to arm or disarm.</p>

      <div v-if="shadows.length" class="mt-4 text-xs opacity-70">
        <span class="uppercase tracking-widest">Per-controller:</span>
        <span v-for="s in shadows" :key="s.key" class="ml-2">
          <code>{{ s.controller }}</code>=<span :class="s.state === 'armed' ? 'text-error font-bold' : ''">{{ s.state }}</span>
        </span>
      </div>
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
        <DataField label="Standing arm">{{ record.arm || 'disarmed' }}</DataField>
        <DataField label="Auto arm">{{ record.auto_arm || '—' }}</DataField>
        <DataField label="Email on intrusion">
          <span class="badge badge-sm" :class="record.notify_on_alarm ? 'badge-warning' : 'badge-ghost'">
            {{ record.notify_on_alarm ? 'emails opted-in operators' : 'off' }}
          </span>
        </DataField>
        <DataField label="Auto schedule">
          <router-link v-if="record.expand?.auto_schedule" :to="`/schedules/${record.expand.auto_schedule.id}`" class="link link-primary">
            {{ record.expand.auto_schedule.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
      </div>
    </BaseCard>

    <BaseCard title="Member inputs">
      <p class="text-sm opacity-60 mb-3">
        Aux inputs assigned to this area. Edit membership on the aux input.
        Only <code>intrusion</code> and <code>tamper_24h</code> points raise alarms.
      </p>
      <div v-if="members.length === 0" class="text-sm opacity-50">No member inputs yet.</div>
      <ul v-else class="divide-y divide-base-200">
        <li v-for="m in members" :key="m.id" class="flex items-center justify-between py-2">
          <router-link :to="`/aux-inputs/${m.id}`" class="link">
            <code class="text-sm">{{ m.code }}</code>
            <span class="opacity-60 ml-2">{{ m.name }}</span>
          </router-link>
          <div class="flex items-center gap-2">
            <code v-if="m.expand?.controller" class="text-xs opacity-60">{{ m.expand.controller.code }}</code>
            <span class="badge badge-sm" :class="pointTypeBadge(m.point_type)">{{ m.point_type || 'monitor' }}</span>
          </div>
        </li>
      </ul>
    </BaseCard>

    <BaseCard title="Member doors">
      <p class="text-sm opacity-60 mb-3">
        Portals assigned to this area. Edit membership on the portal. While armed, a <em>forced</em>
        open (no grant/REX) raises intrusion; a <span class="badge badge-xs badge-warning align-middle">grant disarms</span>
        door also disarms the area on a valid credential.
      </p>
      <div v-if="memberPortals.length === 0" class="text-sm opacity-50">No member doors yet.</div>
      <ul v-else class="divide-y divide-base-200">
        <li v-for="p in memberPortals" :key="p.id" class="flex items-center justify-between py-2">
          <router-link :to="`/portals/${p.id}`" class="link">
            <code class="text-sm">{{ p.code }}</code>
            <span class="opacity-60 ml-2">{{ p.name }}</span>
          </router-link>
          <div class="flex items-center gap-2">
            <code v-if="p.expand?.controller" class="text-xs opacity-60">{{ p.expand.controller.code }}</code>
            <span v-if="p.disarm_on_grant" class="badge badge-sm badge-warning">grant disarms</span>
            <span v-else class="badge badge-sm badge-ghost">monitored</span>
          </div>
        </li>
      </ul>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
