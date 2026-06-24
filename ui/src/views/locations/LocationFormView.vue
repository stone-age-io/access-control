<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Location, HolidayCalendar } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import LocationPicker from '@/components/locations/LocationPicker.vue'
import RelationPicker from '@/components/ui/RelationPicker.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  code: '',
  name: '',
  timezone: 'America/New_York',
  fai_suppress: true,
  notify_fire: false,
  description: '',
  lat: 0,
  lon: 0,
  holiday_calendars: [] as string[],
})

const calendars = ref<HolidayCalendar[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

// Floor-plan image: existing filename, a newly chosen file, and a remove flag.
const existingFloorplan = ref('')
const selectedFile = ref<File | null>(null)
const removeFloorplan = ref(false)

function onFileChange(e: Event) {
  selectedFile.value = (e.target as HTMLInputElement).files?.[0] ?? null
  if (selectedFile.value) removeFloorplan.value = false
}

const kvKey = computed(() => policyKey('locations', { code: form.value.code.trim() }))

// A short list of common IANA zones for the datalist; any valid IANA name works.
const commonTimezones = [
  'UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Phoenix',
  'America/Toronto',
  'America/Sao_Paulo',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Madrid',
  'Asia/Tokyo',
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Kolkata',
  'Australia/Sydney',
]

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const location = await pb.collection('locations').getOne<Location>(recordId)
    form.value = {
      code: location.code || '',
      name: location.name || '',
      timezone: location.timezone || 'UTC',
      fai_suppress: location.fai_suppress ?? true,
      notify_fire: !!location.notify_fire,
      description: location.description || '',
      lat: location.coordinates?.lat ?? 0,
      lon: location.coordinates?.lon ?? 0,
      holiday_calendars: [...(location.holiday_calendars || [])],
    }
    existingFloorplan.value = location.floorplan || ''
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load location')
    router.push('/locations')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.timezone.trim()) { toast.error('Timezone is required'); return }

  loading.value = true
  try {
    const data: Record<string, any> = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      timezone: form.value.timezone.trim(),
      fai_suppress: form.value.fai_suppress,
      notify_fire: form.value.notify_fire,
      description: form.value.description.trim(),
      coordinates: { lat: Number(form.value.lat) || 0, lon: Number(form.value.lon) || 0 },
      holiday_calendars: form.value.holiday_calendars,
    }
    // Clear the existing image only when removing and not replacing it.
    if (removeFloorplan.value && !selectedFile.value) data.floorplan = null

    let id = recordId
    if (isEdit.value) {
      await pb.collection('locations').update(recordId!, data)
    } else {
      const created = await pb.collection('locations').create<Location>(data)
      id = created.id
    }

    // Upload the floor-plan image in a dedicated multipart request so the
    // geoPoint above always travels as clean JSON. maxSelect:1 means a new
    // upload replaces any existing image.
    if (selectedFile.value && id) {
      const fd = new FormData()
      fd.append('floorplan', selectedFile.value)
      await pb.collection('locations').update(id, fd)
    }

    toast.success(isEdit.value ? 'Location updated' : 'Location created')
    router.push(`/locations/${id}`)
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save location')
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  try {
    calendars.value = await pb.collection('holiday_calendars').getFullList<HolidayCalendar>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load holiday calendars')
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
      :title="isEdit ? 'Edit Location' : 'New Location'"
      :breadcrumbs="[{ label: 'Locations', to: '/locations' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'location.<code>'"
    >
      <BaseCard title="Location">
        <div class="space-y-4">
          <FormField label="Code" required hint="Stable slug used in NATS subjects and as the KV key. Avoid spaces.">
            <input v-model="form.code" type="text" placeholder="hq" class="input input-bordered font-mono" required />
          </FormField>

          <FormField label="Name">
            <input v-model="form.name" type="text" placeholder="Headquarters" class="input input-bordered" />
          </FormField>

          <FormField label="Timezone" required hint="IANA timezone name. Used to evaluate schedule windows in local time (handles DST).">
            <input v-model="form.timezone" list="tz-list" type="text" placeholder="America/New_York" class="input input-bordered font-mono" required />
            <datalist id="tz-list">
              <option v-for="tz in commonTimezones" :key="tz" :value="tz" />
            </datalist>
          </FormField>

          <FormField inline label="Suppress alarms while fire input is active (FAI)" hint="Hardware owns egress; software only suppresses false forced/held-open alarms during fire.">
            <input v-model="form.fai_suppress" type="checkbox" class="toggle toggle-primary" />
          </FormField>

          <FormField inline label="Email on fire input" hint="Email opted-in operators when this location's fire input activates. Recipients are set per operator (Operators → Notify).">
            <input v-model="form.notify_fire" type="checkbox" class="toggle toggle-primary" />
          </FormField>

          <FormField label="Holiday calendars" hint="Calendars this site observes. Their dates close holiday-observing schedules here; multiple calendars compose (e.g. national + site-specific).">
            <RelationPicker
              v-model="form.holiday_calendars"
              :options="calendars"
              :primary="(c) => c.code"
              :secondary="(c) => c.name"
              empty="No holiday calendars exist yet — create one under Holiday Calendars."
            />
          </FormField>

          <FormField label="Description" hint="Optional notes about this location.">
            <textarea v-model="form.description" rows="2" placeholder="e.g. Main office — 3 floors" class="textarea textarea-bordered"></textarea>
          </FormField>

          <FormField label="Coordinates" hint="Latitude / longitude for the location map. Leave at 0, 0 if unmapped.">
            <div class="space-y-2">
              <LocationPicker v-model:lat="form.lat" v-model:lon="form.lon" />
              <div class="flex gap-2">
                <input v-model.number="form.lat" type="number" step="any" placeholder="Latitude" class="input input-bordered font-mono flex-1" />
                <input v-model.number="form.lon" type="number" step="any" placeholder="Longitude" class="input input-bordered font-mono flex-1" />
              </div>
            </div>
          </FormField>

          <FormField label="Floor plan" hint="Image (PNG/JPEG/WebP/SVG) used to place portals. A new upload replaces any existing plan.">
            <div class="space-y-2">
              <div v-if="existingFloorplan && !selectedFile && !removeFloorplan" class="flex items-center gap-2 text-sm min-w-0">
                <span class="badge badge-ghost shrink-0">Current</span>
                <code class="truncate">{{ existingFloorplan }}</code>
                <button type="button" class="btn btn-xs btn-ghost text-error shrink-0" @click="removeFloorplan = true">Remove</button>
              </div>
              <div v-else-if="removeFloorplan" class="flex items-center gap-2 text-sm text-base-content/70">
                <span>Floor plan will be removed on save.</span>
                <button type="button" class="btn btn-xs btn-ghost" @click="removeFloorplan = false">Undo</button>
              </div>
              <input
                type="file"
                accept="image/png,image/jpeg,image/webp,image/svg+xml"
                class="file-input file-input-bordered w-full"
                @change="onFileChange"
              />
            </div>
          </FormField>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Location</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
