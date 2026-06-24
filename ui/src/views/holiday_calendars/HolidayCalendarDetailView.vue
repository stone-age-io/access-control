<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { formatDate } from '@/utils/format'
import type { HolidayCalendar, Holiday, Location } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<HolidayCalendar | null>(null)
const holidays = ref<Holiday[]>([])
const observers = ref<Location[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.code || 'Holiday Calendar')

async function load() {
  loading.value = true
  try {
    record.value = await pb.collection('holiday_calendars').getOne<HolidayCalendar>(recordId)
    // Dates on this calendar, and the locations that observe it.
    ;[holidays.value, observers.value] = await Promise.all([
      pb.collection('holidays').getFullList<Holiday>({ filter: `calendar = "${recordId}"`, sort: 'date' }),
      pb.collection('locations').getFullList<Location>({ filter: `holiday_calendars ~ "${recordId}"`, sort: 'code' }),
    ])
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load calendar')
    router.push('/holiday-calendars')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Calendar',
    message: `Delete holiday calendar "${record.value.code}"?`,
    details: 'Its holidays are removed and any location that observes it stops closing on those dates. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('holiday_calendars').delete(recordId)
    toast.success('Calendar deleted')
    router.push('/holiday-calendars')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete calendar')
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
    :breadcrumbs="[{ label: 'Holiday Calendars', to: '/holiday-calendars' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/holiday-calendars/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code"><code class="text-sm">{{ record.code }}</code></DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
      </div>
    </BaseCard>

    <BaseCard title="Holidays">
      <template #actions>
        <router-link :to="`/holidays/new?calendar=${record.id}`" class="btn btn-xs btn-primary">+ Add date</router-link>
      </template>
      <ul v-if="holidays.length" class="divide-y divide-base-200">
        <li v-for="h in holidays" :key="h.id" class="flex items-center gap-3 py-2">
          <router-link :to="`/holidays/${h.id}`" class="font-mono text-sm link link-hover">{{ formatDate(h.date, 'PP') }}</router-link>
          <span class="text-sm opacity-70">{{ h.name || '—' }}</span>
          <span class="badge badge-sm ml-auto" :class="h.recurring ? 'badge-success' : 'badge-ghost'">
            {{ h.recurring ? 'yearly' : 'once' }}
          </span>
        </li>
      </ul>
      <p v-else class="text-sm opacity-50">No dates on this calendar yet.</p>
    </BaseCard>

    <BaseCard title="Observed by">
      <div v-if="observers.length" class="flex flex-wrap gap-2">
        <router-link v-for="l in observers" :key="l.id" :to="`/locations/${l.id}`" class="badge badge-outline badge-lg gap-1">
          <code class="text-xs">{{ l.code }}</code>
        </router-link>
      </div>
      <p v-else class="text-sm opacity-50">No locations observe this calendar yet — add it on a location’s page.</p>
    </BaseCard>

    <RecordMeta :record="record" />
  </DetailLayout>
</template>
