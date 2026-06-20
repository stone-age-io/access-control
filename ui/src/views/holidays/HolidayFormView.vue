<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Holiday, Location } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  // Prefill the location when arriving from a location page (?location=<id>).
  location: (route.query.location as string) || '',
  date: '',
  name: '',
  recurring: false,
})

const locations = ref<Location[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

// Holidays are keyed in KV by record id, which only exists once saved.
const kvKey = computed(() => (recordId ? policyKey('holidays', { id: recordId }) : ''))

async function loadOptions() {
  try {
    locations.value = await pb.collection('locations').getFullList<Location>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load locations')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const h = await pb.collection('holidays').getOne<Holiday>(recordId)
    form.value = {
      location: h.location || '',
      // PocketBase stores "YYYY-MM-DD HH:MM:SS.sssZ"; a date input wants "YYYY-MM-DD".
      date: (h.date || '').slice(0, 10),
      name: h.name || '',
      recurring: !!h.recurring,
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load holiday')
    router.push('/holidays')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.location) { toast.error('Location is required'); return }
  if (!form.value.date) { toast.error('Date is required'); return }

  loading.value = true
  try {
    const data = {
      location: form.value.location,
      date: form.value.date,
      name: form.value.name.trim(),
      recurring: form.value.recurring,
    }
    if (isEdit.value) {
      await pb.collection('holidays').update(recordId!, data)
      toast.success('Holiday updated')
      router.push(`/holidays/${recordId}`)
    } else {
      const created = await pb.collection('holidays').create<Holiday>(data)
      toast.success('Holiday created')
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
          <FormField label="Location" required>
            <select v-model="form.location" class="select select-bordered" required>
              <option value="">Select a location...</option>
              <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
            </select>
            <p v-if="locations.length === 0" class="text-xs text-warning">No locations exist yet — create one first.</p>
          </FormField>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Date" required>
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
