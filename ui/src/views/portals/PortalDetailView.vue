<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { usePortalCommands, POSTURES } from '@/composables/usePortalCommands'
import { policyKey } from '@/utils/policyKey'
import type { Portal, AccessGroup, PointStatus } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import PostureBadge from '@/components/ui/PostureBadge.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Portal | null>(null)
const groups = ref<AccessGroup[]>([])
const loading = ref(true)
const deleting = ref(false)

// Live status (ACC_STATUS device shadow, projected into point_status).
const status = ref<PointStatus | null>(null)
const { commanding, grant, setPosture } = usePortalCommands()
let unsubStatus: (() => void) | null = null

const title = computed(() => record.value?.name || record.value?.code || 'Portal')
// A manual override is in force — gates (and highlights) the "Clear override" action.
const isOverridden = computed(() => status.value?.posture_source === 'override')
const kvKey = computed(() => (record.value ? policyKey('portals', record.value) : ''))
const statusKey = computed(() => (record.value ? `portal.${record.value.code}` : ''))

const doorBadge = computed(() => {
  switch (status.value?.state) {
    case 'open':
      return { cls: 'badge-error', text: 'Open' }
    case 'closed':
      return { cls: 'badge-success', text: 'Closed' }
    default:
      return { cls: 'badge-ghost', text: 'Unknown' }
  }
})

function changedAt(): string {
  if (!status.value?.changed) return '—'
  const d = new Date(status.value.changed)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    const [p, g] = await Promise.all([
      pb.collection('portals').getOne<Portal>(recordId, { expand: 'location,controller,auto_schedule' }),
      pb.collection('access_groups').getFullList<AccessGroup>({ filter: `portals ~ "${recordId}"`, sort: 'code' }),
    ])
    record.value = p
    groups.value = g
    await loadStatus()
    await subscribeStatus()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load portal')
    router.push('/portals')
  } finally {
    loading.value = false
  }
}

// Fetch the current shadow row (if the controller has reported any).
async function loadStatus() {
  try {
    status.value = await pb
      .collection('point_status')
      .getFirstListItem<PointStatus>(`key = "${statusKey.value}"`)
  } catch {
    status.value = null // no shadow yet (controller offline / not reporting)
  }
}

// Live updates: point_status is small, so subscribe to all and filter by key.
async function subscribeStatus() {
  unsubStatus = await pb.collection('point_status').subscribe<PointStatus>('*', (e) => {
    if (e.record.key !== statusKey.value) return
    status.value = e.action === 'delete' ? null : e.record
  })
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
    :breadcrumbs="[{ label: 'Portals', to: '/portals' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/portals/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Identity">
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
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
      </div>
    </BaseCard>

    <BaseCard title="Live status">
      <div v-if="status" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-x-6 gap-y-4">
        <DataField label="Door">
          <span class="badge badge-sm" :class="doorBadge.cls">{{ doorBadge.text }}</span>
        </DataField>
        <DataField label="Effective posture">
          <PostureBadge :posture="status.posture" :source="status.posture_source" />
        </DataField>
        <DataField label="Held open">
          <span v-if="status.held" class="badge badge-sm badge-warning">Held</span>
          <span v-else class="opacity-40">No</span>
        </DataField>
        <DataField label="Updated">{{ changedAt() }}</DataField>
      </div>
      <p v-else class="text-sm opacity-50">
        No live status yet — the controller driving this portal hasn’t reported (offline or unassigned).
      </p>
    </BaseCard>

    <BaseCard title="Controls">
      <div class="space-y-4">
        <div>
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Momentary</div>
          <button class="btn btn-sm btn-primary" :disabled="commanding" @click="grant(recordId)">
            Grant (unlock once)
          </button>
        </div>
        <div class="border-t border-base-200 pt-4">
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Posture override</div>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="p in POSTURES"
              :key="p.value"
              class="btn btn-sm"
              :class="p.danger ? 'btn-outline btn-warning' : 'btn-outline'"
              :disabled="commanding"
              @click="setPosture(recordId, p.value, { danger: p.danger, code: record.code })"
            >
              {{ p.label }}
            </button>
            <button
              class="btn btn-sm"
              :class="isOverridden ? 'btn-outline btn-warning' : 'btn-ghost'"
              :disabled="commanding || !isOverridden"
              @click="setPosture(recordId, 'clear')"
            >
              Clear override
            </button>
          </div>
          <p class="text-xs opacity-50 mt-2">
            <span v-if="isOverridden" class="text-warning font-medium">A manual override is in force. </span>
            A posture override is operational state on the controller — it is not saved to this record, and
            “Clear” reverts to the scheduled or standing posture.
          </p>
        </div>
      </div>
    </BaseCard>

    <BaseCard title="Posture &amp; timing">
      <div class="space-y-4">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
          <DataField label="Standing posture">
            <span class="badge badge-sm badge-ghost">{{ record.posture || 'secure' }}</span>
          </DataField>
          <DataField label="Pulse">{{ record.pulse_seconds }} s</DataField>
        </div>
        <div class="border-t border-base-200 pt-4">
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Scheduled override</div>
          <p v-if="!record.auto_posture && !record.auto_schedule" class="text-sm opacity-50">
            None — the standing posture always applies.
          </p>
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
        </div>
      </div>
    </BaseCard>

    <BaseCard title="Controller &amp; hardware">
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
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
        <DataField label="Reader">
          <span v-if="record.reader_address >= 0">OSDP @ PD {{ record.reader_address }}</span>
          <span v-else>NATS-only</span>
        </DataField>
      </div>
    </BaseCard>

    <RelationList
      title="In access groups"
      icon="🗝️"
      :items="groups"
      :to="(g) => `/access-groups/${g.id}`"
      :primary="(g) => g.code"
      :secondary="(g) => g.name"
      empty="Not in any access group yet."
    />

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
