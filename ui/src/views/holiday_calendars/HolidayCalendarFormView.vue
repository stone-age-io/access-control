<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import type { HolidayCalendar } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({ code: '', name: '' })
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})
const { markClean } = useUnsavedChanges(() => form.value)

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const c = await pb.collection('holiday_calendars').getOne<HolidayCalendar>(recordId)
    form.value = { code: c.code || '', name: c.name || '' }
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load calendar')
    router.push('/holiday-calendars')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.code.trim()) e.code = 'Code is required'
  errors.value = e
  const first = Object.values(e)[0]
  if (first) toast.error(first)
  return !first
}

async function handleSubmit() {
  if (!validate()) return

  loading.value = true
  try {
    const data = { code: form.value.code.trim(), name: form.value.name.trim() }
    if (isEdit.value) {
      await pb.collection('holiday_calendars').update(recordId!, data)
      toast.success('Calendar updated')
      markClean()
      router.push(`/holiday-calendars/${recordId}`)
    } else {
      const created = await pb.collection('holiday_calendars').create<HolidayCalendar>(data)
      toast.success('Calendar created')
      markClean()
      router.push(`/holiday-calendars/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save calendar')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (isEdit.value) loadRecord()
})
</script>

<template>
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <FormLayout
      :title="isEdit ? 'Edit Calendar' : 'New Calendar'"
      :breadcrumbs="[{ label: 'Holiday Calendars', to: '/holiday-calendars' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Holiday Calendar">
        <div class="space-y-4">
          <FormField label="Code" required :error="errors.code" hint="Stable slug referenced by holidays and by the locations that observe this calendar.">
            <input v-model="form.code" type="text" placeholder="us-holidays" class="input input-bordered font-mono" required />
          </FormField>
          <FormField label="Name">
            <input v-model="form.name" type="text" placeholder="US Holidays" class="input input-bordered" />
          </FormField>
          <p class="text-sm text-base-content/60">
            Add dates from the calendar’s page after saving, then have one or more locations observe it.
          </p>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Calendar</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
