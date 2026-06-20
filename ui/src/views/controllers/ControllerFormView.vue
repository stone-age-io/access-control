<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Controller, Location, ControllerModel } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const MODELS: ControllerModel[] = ['kincony-server-mini', 'kincony-pi5r8']

const form = ref({
  code: '',
  name: '',
  location: '',
  model: 'kincony-server-mini' as ControllerModel,
})

const locations = ref<Location[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('controllers', { code: form.value.code.trim() }))

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
    const c = await pb.collection('controllers').getOne<Controller>(recordId)
    form.value = {
      code: c.code || '',
      name: c.name || '',
      location: c.location || '',
      model: (c.model || 'kincony-server-mini') as ControllerModel,
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load controller')
    router.push('/controllers')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.location) { toast.error('Location is required'); return }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      location: form.value.location,
      model: form.value.model,
    }
    if (isEdit.value) {
      await pb.collection('controllers').update(recordId!, data)
      toast.success('Controller updated')
      router.push(`/controllers/${recordId}`)
    } else {
      const created = await pb.collection('controllers').create<Controller>(data)
      toast.success('Controller created')
      router.push(`/controllers/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save controller')
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
      :title="isEdit ? 'Edit Controller' : 'New Controller'"
      :breadcrumbs="[{ label: 'Controllers', to: '/controllers' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'controller.<code>'"
    >
      <BaseCard title="Controller">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required hint="Set this as controller.code in the edge box's config.">
              <input v-model="form.code" type="text" placeholder="ctrl-hq-1" class="input input-bordered font-mono" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="HQ Controller 1" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Location" required>
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
              <p v-if="locations.length === 0" class="text-xs text-warning">No locations exist yet — create one first.</p>
            </FormField>
            <FormField label="Model" hint="Hardware template that maps logical relay/input indices to physical lines.">
              <select v-model="form.model" class="select select-bordered">
                <option v-for="mo in MODELS" :key="mo" :value="mo">{{ mo }}</option>
              </select>
            </FormField>
          </div>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Controller</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
