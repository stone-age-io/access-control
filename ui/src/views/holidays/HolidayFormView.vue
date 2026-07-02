<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { policyKey } from '@/utils/policyKey'
import type { Holiday, HolidayCalendar } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  // Prefill the calendar when arriving from a calendar page (?calendar=<id>).
  calendar: (route.query.calendar as string) || '',
  date: '',
  name: '',
  recurring: false,
})

const calendars = ref<HolidayCalendar[]>([])
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})
const { markClean } = useUnsavedChanges(() => form.value)

// Holidays are keyed in KV by record id, which only exists once saved.
const kvKey = computed(() => (recordId ? policyKey('holidays', { id: recordId }) : ''))

async function loadOptions() {
  try {
    calendars.value = await pb.collection('holiday_calendars').getFullList<HolidayCalendar>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load calendars')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const h = await pb.collection('holidays').getOne<Holiday>(recordId)
    form.value = {
      calendar: h.calendar || '',
      // PocketBase stores "YYYY-MM-DD HH:MM:SS.sssZ"; a date input wants "YYYY-MM-DD".
      date: (h.date || '').slice(0, 10),
      name: h.name || '',
      recurring: !!h.recurring,
    }
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load holiday')
    router.push('/holidays')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.calendar) e.calendar = 'Calendar is required'
  if (!form.value.date) e.date = 'Date is required'
  errors.value = e
  const first = Object.values(e)[0]
  if (first) toast.error(first)
  return !first
}

async function handleSubmit() {
  if (!validate()) return

  loading.value = true
  try {
    const data = {
      calendar: form.value.calendar,
      date: form.value.date,
      name: form.value.name.trim(),
      recurring: form.value.recurring,
    }
    if (isEdit.value) {
      await pb.collection('holidays').update(recordId!, data)
      toast.success('Holiday updated')
      markClean()
      router.push(`/holidays/${recordId}`)
    } else {
      const created = await pb.collection('holidays').create<Holiday>(data)
      toast.success('Holiday created')
      markClean()
      router.push(`/holidays/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save holiday')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadOptions()
  if (isEdit.value) await loadRecord()
})
</script>

<template>
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <FormLayout
      :title="isEdit ? 'Edit Holiday' : 'New Holiday'"
      :breadcrumbs="[{ label: 'Holidays', to: '/holidays' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'holiday.<id>'"
    >
      <BaseCard title="Holiday">
        <div class="space-y-4">
          <FormField label="Calendar" required :error="errors.calendar" hint="Which holiday calendar this date belongs to. Locations observe calendars, so one date can serve many sites.">
            <select v-model="form.calendar" class="select select-bordered" required>
              <option value="">Select a calendar...</option>
              <option v-for="c in calendars" :key="c.id" :value="c.id">{{ c.code }} — {{ c.name || c.code }}</option>
            </select>
            <p v-if="calendars.length === 0" class="text-xs text-warning">No calendars exist yet — create one first.</p>
          </FormField>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Date" required :error="errors.date">
              <input v-model="form.date" type="date" class="input input-bordered" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Christmas" class="input input-bordered" />
            </FormField>
          </div>

          <FormField inline label="Recurring" hint="Matches this month/day every year (for fixed-date holidays like Dec 25).">
            <input v-model="form.recurring" type="checkbox" class="toggle toggle-primary" />
          </FormField>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Holiday</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
