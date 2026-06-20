<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Schedule, ScheduleWindow, AccessGroup } from '@/types/pocketbase'
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
const record = ref<Schedule | null>(null)
const groups = ref<AccessGroup[]>([])
const loading = ref(true)
const deleting = ref(false)

// ISO weekdays: 1=Mon .. 7=Sun.
const DAYS = [
  { num: 1, label: 'Mon' },
  { num: 2, label: 'Tue' },
  { num: 3, label: 'Wed' },
  { num: 4, label: 'Thu' },
  { num: 5, label: 'Fri' },
  { num: 6, label: 'Sat' },
  { num: 7, label: 'Sun' },
]

const title = computed(() => record.value?.name || record.value?.code || 'Schedule')
const kvKey = computed(() => (record.value ? policyKey('schedules', record.value) : ''))

function crossesMidnight(w: ScheduleWindow): boolean {
  return !!w.start && !!w.end && w.end <= w.start
}

async function load() {
  loading.value = true
  try {
    const [s, g] = await Promise.all([
      pb.collection('schedules').getOne<Schedule>(recordId),
      pb.collection('access_groups').getFullList<AccessGroup>({ filter: `schedule = "${recordId}"`, sort: 'code' }),
    ])
    record.value = s
    groups.value = g
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load schedule')
    router.push('/schedules')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Schedule',
    message: `Delete schedule "${record.value.code}"?`,
    details: 'Access groups using this schedule will lose their time window. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('schedules').delete(recordId)
    toast.success('Schedule deleted')
    router.push('/schedules')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete schedule')
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
    :breadcrumbs="[{ label: 'Schedules', to: '/schedules' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/schedules/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard title="Schedule">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Observes holidays">
          <span class="badge badge-sm" :class="!record.ignore_holidays ? 'badge-success' : 'badge-ghost'">
            {{ !record.ignore_holidays ? 'Yes' : 'No' }}
          </span>
        </DataField>
      </div>
    </BaseCard>

    <BaseCard title="Time Windows">
      <div v-if="!record.windows || record.windows.length === 0" class="text-center py-6 text-sm opacity-50">
        No windows.
      </div>
      <div v-else class="space-y-4">
        <div
          v-for="(w, idx) in record.windows"
          :key="idx"
          class="rounded-box border border-base-300 p-4 space-y-3"
        >
          <!-- Days -->
          <div class="flex flex-wrap gap-1.5">
            <span
              v-for="d in DAYS"
              :key="d.num"
              class="badge badge-sm"
              :class="w.days.includes(d.num) ? 'badge-primary' : 'badge-ghost opacity-50'"
            >
              {{ d.label }}
            </span>
          </div>

          <!-- Times -->
          <div class="flex flex-wrap items-center gap-2">
            <code class="text-sm font-mono">{{ w.start }} → {{ w.end }}</code>
            <span v-if="crossesMidnight(w)" class="badge badge-warning badge-sm">crosses midnight</span>
          </div>
        </div>
      </div>
    </BaseCard>

    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="Used by groups"
        icon="🗝️"
        :items="groups"
        :to="(g) => `/access-groups/${g.id}`"
        :primary="(g) => g.code"
        :secondary="(g) => g.name"
        empty="Not used by any access group yet."
      />
    </template>
  </DetailLayout>
</template>
