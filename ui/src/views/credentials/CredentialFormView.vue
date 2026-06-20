<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Credential, CredentialType, CredentialStatus, Cardholder } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'

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
  // Prefill the holder when arriving from a cardholder page (?user=<id>).
  user: (route.query.user as string) || '',
  status: 'active' as CredentialStatus,
  label: '',
  // datetime-local strings ("YYYY-MM-DDTHH:MM"); '' = no bound.
  valid_from: '',
  valid_until: '',
})

// PocketBase stores dates as e.g. "2026-12-25 10:30:00.000Z"; a datetime-local
// input wants local "YYYY-MM-DDTHH:MM". Convert in both directions.
function toLocalInput(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fromLocalInput(local: string): string {
  if (!local) return ''
  const d = new Date(local)
  if (isNaN(d.getTime())) return ''
  return d.toISOString()
}

const cardholders = ref<Cardholder[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('credentials', { value: form.value.value.trim() }))

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
      valid_from: toLocalInput(c.valid_from || ''),
      valid_until: toLocalInput(c.valid_until || ''),
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
      valid_from: fromLocalInput(form.value.valid_from),
      valid_until: fromLocalInput(form.value.valid_until),
    }
    if (isEdit.value) {
      await pb.collection('credentials').update(recordId!, data)
      toast.success('Credential updated')
      router.push(`/credentials/${recordId}`)
    } else {
      const created = await pb.collection('credentials').create<Credential>(data)
      toast.success('Credential created')
      // Return to the holder when we came from one; otherwise the new credential.
      router.push(form.value.user ? `/cardholders/${form.value.user}` : `/credentials/${created.id}`)
    }
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
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <DetailLayout
      :title="isEdit ? 'Edit Credential' : 'New Credential'"
      :breadcrumbs="[{ label: 'Credentials', to: '/credentials' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
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

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Valid from</span></label>
              <input v-model="form.valid_from" type="datetime-local" class="input input-bordered" />
              <label class="label"><span class="label-text-alt">Leave blank for no bound.</span></label>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Valid until</span></label>
              <input v-model="form.valid_until" type="datetime-local" class="input input-bordered" />
              <label class="label"><span class="label-text-alt">Leave blank for no bound.</span></label>
            </div>
          </div>
        </div>
      </BaseCard>

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">cred.&lt;value&gt;</code>
          <p class="text-xs opacity-50">The reader presents this value; the controller looks it up by this key.</p>
        </RailCard>
        <RailCard title="About credentials" icon="🎫">
          <p class="text-xs opacity-60 leading-relaxed">
            A credential is an opaque string mapped to one cardholder. Revoke or suspend it to stop access
            without deleting the record.
          </p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Credential</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
