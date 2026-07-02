<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { policyKey } from '@/utils/policyKey'
import type { Area, Location, Schedule } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  code: '',
  name: '',
  location: (route.query.location as string) || '',
  arm: 'disarmed' as 'disarmed' | 'armed',
  auto_arm: '' as '' | 'disarmed' | 'armed',
  auto_schedule: '',
  notify_on_alarm: false,
})

const locations = ref<Location[]>([])
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})
const { markClean } = useUnsavedChanges(() => form.value)

const kvKey = computed(() => policyKey('areas', { code: form.value.code }))

async function loadOptions() {
  try {
    const [locs, scheds] = await Promise.all([
      pb.collection('locations').getFullList<Location>({ sort: 'code' }),
      pb.collection('schedules').getFullList<Schedule>({ sort: 'code' }),
    ])
    locations.value = locs
    schedules.value = scheds
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const a = await pb.collection('areas').getOne<Area>(recordId)
    form.value = {
      code: a.code || '',
      name: a.name || '',
      location: a.location || '',
      arm: a.arm === 'armed' ? 'armed' : 'disarmed',
      auto_arm: a.auto_arm || '',
      auto_schedule: a.auto_schedule || '',
      notify_on_alarm: !!a.notify_on_alarm,
    }
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load area')
    router.push('/areas')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.code.trim()) e.code = 'Code is required'
  if (!form.value.location) e.location = 'Location is required'
  // Both-or-neither: a scheduled arm needs a schedule, and vice versa. The mirror
  // drops a half-configured pair anyway; reject early for a clear message.
  if (!!form.value.auto_arm !== !!form.value.auto_schedule) {
    e.auto_arm = 'Scheduled arm needs both an auto-arm state and a schedule (or neither)'
    e.auto_schedule = 'Scheduled arm needs both an auto-arm state and a schedule (or neither)'
  }
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
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      location: form.value.location,
      arm: form.value.arm,
      auto_arm: form.value.auto_arm,
      auto_schedule: form.value.auto_schedule,
      notify_on_alarm: form.value.notify_on_alarm,
    }
    if (isEdit.value) {
      await pb.collection('areas').update(recordId!, data)
      toast.success('Area updated')
      markClean()
      router.push(`/areas/${recordId}`)
    } else {
      const created = await pb.collection('areas').create<Area>(data)
      toast.success('Area created')
      markClean()
      router.push(`/areas/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save area')
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
      :title="isEdit ? 'Edit Area' : 'New Area'"
      :breadcrumbs="[{ label: 'Areas', to: '/areas' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'area.<code>'"
    >
      <BaseCard title="Area">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required :error="errors.code">
              <input v-model="form.code" type="text" placeholder="warehouse" class="input input-bordered" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Warehouse" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Location" required :error="errors.location" hint="An area is single-location; member inputs should be on controllers at this location.">
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
            </FormField>
            <FormField label="Standing arm" hint="The default arm-state when no override or schedule applies.">
              <select v-model="form.arm" class="select select-bordered">
                <option value="disarmed">Disarmed</option>
                <option value="armed">Armed</option>
              </select>
            </FormField>
          </div>

          <FormField inline label="Email on intrusion"
                     hint="Email opted-in operators when this area raises an intrusion alarm. Recipients are set per operator (Operators → Notify).">
            <input v-model="form.notify_on_alarm" type="checkbox" class="toggle toggle-primary" />
          </FormField>
        </div>
      </BaseCard>

      <BaseCard title="Scheduled arming">
        <p class="text-sm opacity-60 mb-4">
          Optional: arm/disarm automatically while a schedule's window is open. An operator override always wins.
        </p>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <FormField label="Auto arm-state" :error="errors.auto_arm" hint="Applied while the schedule below is open.">
            <select v-model="form.auto_arm" class="select select-bordered">
              <option value="">No automation</option>
              <option value="armed">Armed</option>
              <option value="disarmed">Disarmed</option>
            </select>
          </FormField>
          <FormField label="Auto schedule" :error="errors.auto_schedule" hint="Gates the auto arm-state (both-or-neither).">
            <select v-model="form.auto_schedule" class="select select-bordered">
              <option value="">None</option>
              <option v-for="s in schedules" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
            </select>
          </FormField>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Area</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
