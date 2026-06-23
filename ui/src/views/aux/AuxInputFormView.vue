<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { AuxInput, Location, Controller } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import IndexPicker from '@/components/ui/IndexPicker.vue'
import { useControllerIO } from '@/composables/useControllerIO'
import { conflictsAt } from '@/utils/io'

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
  input_index: 0,
  contact: 'no' as 'no' | 'nc',
})

const locations = ref<Location[]>([])
const controllers = ref<Controller[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('aux_input', { code: form.value.code }))

// The monitoring controller's capacity + occupancy, for the input-index picker.
const ctrlId = computed(() => form.value.controller)
const { profile, io } = useControllerIO(ctrlId)
const inputLines = computed(() => profile.value?.inputs ?? [])

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
    const a = await pb.collection('aux_input').getOne<AuxInput>(recordId)
    form.value = {
      code: a.code || '',
      name: a.name || '',
      location: a.location || '',
      controller: a.controller || '',
      input_index: a.input_index || 0,
      contact: a.contact === 'nc' ? 'nc' : 'no',
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load aux input')
    router.push('/aux-inputs')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.location) { toast.error('Location is required'); return }
  const taken = conflictsAt(io.value.inputs, form.value.input_index, recordId)
  if (taken.length) {
    toast.error(`Input ${form.value.input_index} already used by ${taken.map((o) => o.label).join(', ')} on this controller`); return
  }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      location: form.value.location,
      controller: form.value.controller,
      input_index: Number(form.value.input_index) || 0,
      contact: form.value.contact,
    }
    if (isEdit.value) {
      await pb.collection('aux_input').update(recordId!, data)
      toast.success('Aux input updated')
      router.push(`/aux-inputs/${recordId}`)
    } else {
      const created = await pb.collection('aux_input').create<AuxInput>(data)
      toast.success('Aux input created')
      router.push(`/aux-inputs/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save aux input')
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
      :title="isEdit ? 'Edit Aux Input' : 'New Aux Input'"
      :breadcrumbs="[{ label: 'Aux Inputs', to: '/aux-inputs' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'auxin.<code>'"
    >
      <BaseCard title="Aux input">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required>
              <input v-model="form.code" type="text" placeholder="dock-contact" class="input input-bordered" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Loading Dock Contact" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Location" required>
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
            </FormField>
            <FormField label="Controller" hint="The edge box that monitors this input (matched by code).">
              <select v-model="form.controller" class="select select-bordered">
                <option value="">Unassigned</option>
                <option v-for="c in controllers" :key="c.id" :value="c.id">{{ c.code }} — {{ c.name || c.code }}</option>
              </select>
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Input index" hint="The box's input line to monitor; the picker lists the model's lines and flags any already in use.">
              <IndexPicker v-model="form.input_index" :lines="inputLines" :usage="io.inputs" :self-id="recordId" />
            </FormField>
            <FormField label="Contact" hint="Normally open (closes when asserted) is typical. Choose normally closed for a supervised contact that opens when asserted.">
              <select v-model="form.contact" class="select select-bordered">
                <option value="no">Normally open (N.O.)</option>
                <option value="nc">Normally closed (N.C.)</option>
              </select>
            </FormField>
          </div>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Aux Input</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
