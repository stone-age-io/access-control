<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import { formatDate } from '@/utils/format'
import type { Holiday } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Holiday | null>(null)
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || (record.value ? formatDate(record.value.date, 'PP') : 'Holiday'))
const kvKey = computed(() => (record.value ? policyKey('holidays', record.value) : ''))

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('holidays').getOne<Holiday>(recordId, { expand: 'calendar' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load holiday')
    router.push('/holidays')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Holiday',
    message: `Delete holiday "${record.value.name || record.value.date}"?`,
    details: 'Schedules that observe holidays will reopen on this day. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('holidays').delete(recordId)
    toast.success('Holiday deleted')
    router.push('/holidays')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete holiday')
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
    :breadcrumbs="[{ label: 'Holidays', to: '/holidays' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/holidays/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard>
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Date">
          <code class="text-sm">{{ formatDate(record.date, 'PP') }}</code>
        </DataField>
        <DataField label="Calendar">
          <router-link v-if="record.expand?.calendar" :to="`/holiday-calendars/${record.expand.calendar.id}`" class="link link-primary">
            {{ record.expand.calendar.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Recurring">
          <SoftBadge :tone="record.recurring ? 'success' : 'neutral'" dot>
            {{ record.recurring ? 'yearly (month/day)' : 'once' }}
          </SoftBadge>
        </DataField>
      </div>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
