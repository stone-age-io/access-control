<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Location } from '@/types/pocketbase'
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
  timezone: 'America/New_York',
  fai_suppress: true,
})

const loading = ref(false)
const loadingRecord = ref(false)

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
    }
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
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      timezone: form.value.timezone.trim(),
      fai_suppress: form.value.fai_suppress,
    }
    if (isEdit.value) {
      await pb.collection('locations').update(recordId!, data)
      toast.success('Location updated')
      router.push(`/locations/${recordId}`)
    } else {
      const created = await pb.collection('locations').create<Location>(data)
      toast.success('Location created')
      router.push(`/locations/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save location')
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
