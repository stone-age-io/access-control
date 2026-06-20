<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { AuxOutput, Location, Controller } from '@/types/pocketbase'
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
  controller: (route.query.controller as string) || '',
  relay_index: 0,
  pulse_seconds: 5,
})

const locations = ref<Location[]>([])
const controllers = ref<Controller[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('aux_output', { code: form.value.code }))

async function loadOptions() {
  try {
    const [locs, ctrls] = await Promise.all([
      pb.collection('locations').getFullList<Location>({ sort: 'code' }),
      pb.collection('controllers').getFullList<Controller>({ sort: 'code' }),
    ])
    locations.value = locs
    controllers.value = ctrls
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const a = await pb.collection('aux_output').getOne<AuxOutput>(recordId)
    form.value = {
      code: a.code || '',
      name: a.name || '',
      location: a.location || '',
      controller: a.controller || '',
      relay_index: a.relay_index || 0,
      pulse_seconds: a.pulse_seconds || 0,
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load aux output')
    router.push('/aux-outputs')
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
      controller: form.value.controller,
      relay_index: Number(form.value.relay_index) || 0,
      pulse_seconds: Number(form.value.pulse_seconds) || 0,
    }
    if (isEdit.value) {
      await pb.collection('aux_output').update(recordId!, data)
      toast.success('Aux output updated')
      router.push(`/aux-outputs/${recordId}`)
    } else {
      const created = await pb.collection('aux_output').create<AuxOutput>(data)
      toast.success('Aux output created')
      router.push(`/aux-outputs/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save aux output')
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
      :title="isEdit ? 'Edit Aux Output' : 'New Aux Output'"
      :breadcrumbs="[{ label: 'Aux Outputs', to: '/aux-outputs' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'auxout.<code>'"
    >
      <BaseCard title="Aux output">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required>
              <input v-model="form.code" type="text" placeholder="gate-strike" class="input input-bordered" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Parking Gate Strike" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Location" required>
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
            </FormField>
            <FormField label="Controller" hint="The edge box that drives this relay (matched by code).">
              <select v-model="form.controller" class="select select-bordered">
                <option value="">Unassigned</option>
                <option v-for="c in controllers" :key="c.id" :value="c.id">{{ c.code }} — {{ c.name || c.code }}</option>
              </select>
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Relay index" hint="Logical relay index on the box.">
              <input v-model.number="form.relay_index" type="number" min="0" class="input input-bordered w-32" />
            </FormField>
            <FormField label="Pulse seconds" hint="Default duration for a momentary pulse.">
              <input v-model.number="form.pulse_seconds" type="number" min="0" class="input input-bordered w-32" />
            </FormField>
          </div>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Aux Output</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
