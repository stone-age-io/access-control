<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Credential, CredentialType, CredentialStatus, Cardholder } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const TYPES: CredentialType[] = ['nkey', 'wiegand', 'pin', 'mobile']
const STATUSES: CredentialStatus[] = ['active', 'revoked', 'suspended']

const form = ref({
  value: '',
  type: 'wiegand' as CredentialType,
  user: '',
  status: 'active' as CredentialStatus,
  label: '',
})

const cardholders = ref<Cardholder[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

async function loadOptions() {
  try {
    cardholders.value = await pb.collection('cardholders').getFullList<Cardholder>({ sort: 'name' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load cardholders')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const c = await pb.collection('credentials').getOne<Credential>(recordId)
    form.value = {
      value: c.value || '',
      type: (c.type || 'wiegand') as CredentialType,
      user: c.user || '',
      status: (c.status || 'active') as CredentialStatus,
      label: c.label || '',
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load credential')
    router.push('/credentials')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.value.trim()) { toast.error('Value is required'); return }
  if (!form.value.user) { toast.error('Cardholder is required'); return }

  loading.value = true
  try {
    const data = {
      value: form.value.value.trim(),
      type: form.value.type,
      user: form.value.user,
      status: form.value.status,
      label: form.value.label.trim(),
    }
    if (isEdit.value) {
      await pb.collection('credentials').update(recordId!, data)
      toast.success('Credential updated')
    } else {
      await pb.collection('credentials').create(data)
      toast.success('Credential created')
    }
    router.push('/credentials')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save credential')
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
  <div class="space-y-6 max-w-2xl">
    <div>
      <div class="breadcrumbs text-sm">
        <ul>
          <li><router-link to="/credentials">Credentials</router-link></li>
          <li>{{ isEdit ? 'Edit' : 'New' }}</li>
        </ul>
      </div>
      <h1 class="text-3xl font-bold">{{ isEdit ? 'Edit Credential' : 'New Credential' }}</h1>
    </div>

    <div v-if="loadingRecord" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-6">
      <BaseCard title="Credential">
        <div class="space-y-4">
          <div class="form-control">
            <label class="label"><span class="label-text">Value *</span></label>
            <input v-model="form.value" type="text" placeholder="CARD-001" class="input input-bordered font-mono" required />
            <label class="label"><span class="label-text-alt">The exact string presented at the reader. Used as the KV key — avoid spaces.</span></label>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Type</span></label>
              <select v-model="form.type" class="select select-bordered">
                <option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option>
              </select>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Status</span></label>
              <select v-model="form.status" class="select select-bordered">
                <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
              </select>
            </div>
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Cardholder *</span></label>
            <select v-model="form.user" class="select select-bordered" required>
              <option value="">Select a cardholder...</option>
              <option v-for="c in cardholders" :key="c.id" :value="c.id">{{ c.name || c.email || c.id }}</option>
            </select>
            <label v-if="cardholders.length === 0" class="label">
              <span class="label-text-alt text-warning">No cardholders exist yet — create one first.</span>
            </label>
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Label</span></label>
            <input v-model="form.label" type="text" placeholder="Alice's badge" class="input input-bordered" />
          </div>
        </div>
      </BaseCard>

      <div class="flex flex-col sm:flex-row justify-end gap-2 sm:gap-4">
        <button type="button" @click="router.back()" class="btn btn-ghost order-2 sm:order-1" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary order-1 sm:order-2" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Credential</span>
        </button>
      </div>
    </form>
  </div>
</template>
