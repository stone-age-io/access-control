<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { policyKey } from '@/utils/policyKey'
import type { Credential, CredentialType, CredentialStatus, Cardholder } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const TYPES: CredentialType[] = ['generic', 'wiegand', 'pin', 'mobile']
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

// uuidv7 mints a time-ordered RFC 9562 UUIDv7: a 48-bit millisecond timestamp
// followed by 74 random bits (version/variant fixed). Time-ordering keeps newly
// enrolled credentials roughly sortable in listings/KV. Used only for the opt-in
// "Generate" button — handy for system-assigned credentials (mobile/virtual),
// never for a physical card whose value is dictated by the reader.
function uuidv7(): string {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  const ts = Date.now()
  // 48-bit big-endian timestamp in bytes 0..5.
  b[0] = (ts / 2 ** 40) & 0xff
  b[1] = (ts / 2 ** 32) & 0xff
  b[2] = (ts / 2 ** 24) & 0xff
  b[3] = (ts / 2 ** 16) & 0xff
  b[4] = (ts / 2 ** 8) & 0xff
  b[5] = ts & 0xff
  b[6] = (b[6] & 0x0f) | 0x70 // version 7
  b[8] = (b[8] & 0x3f) | 0x80 // variant 10
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

function generateValue() {
  form.value.value = uuidv7()
}

const cardholders = ref<Cardholder[]>([])
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})
const { markClean } = useUnsavedChanges(() => form.value)

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
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load credential')
    router.push('/credentials')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.value.trim()) e.value = 'Value is required'
  if (!form.value.user) e.user = 'Cardholder is required'
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
      markClean()
      router.push(`/credentials/${recordId}`)
    } else {
      const created = await pb.collection('credentials').create<Credential>(data)
      toast.success('Credential created')
      markClean()
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
    <FormLayout
      :title="isEdit ? 'Edit Credential' : 'New Credential'"
      :breadcrumbs="[{ label: 'Credentials', to: '/credentials' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'cred.<value>'"
    >
      <BaseCard title="Credential">
        <div class="space-y-4">
          <FormField label="Value" required :error="errors.value" hint="The exact string presented at the reader. Used as the KV key — avoid spaces. Generate only for system-assigned credentials (mobile/virtual); a physical card's value is fixed by the reader.">
            <div class="join w-full">
              <input v-model="form.value" type="text" placeholder="CARD-001" class="input input-bordered font-mono join-item flex-1" required />
              <button type="button" class="btn btn-outline join-item" @click="generateValue" title="Generate a UUIDv7 value">Generate</button>
            </div>
          </FormField>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Type">
              <select v-model="form.type" class="select select-bordered">
                <option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option>
              </select>
            </FormField>
            <FormField label="Status">
              <select v-model="form.status" class="select select-bordered">
                <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
              </select>
            </FormField>
          </div>

          <FormField label="Cardholder" required :error="errors.user">
            <select v-model="form.user" class="select select-bordered" required>
              <option value="">Select a cardholder...</option>
              <option v-for="c in cardholders" :key="c.id" :value="c.id">{{ c.name || c.email || c.id }}</option>
            </select>
            <p v-if="cardholders.length === 0" class="text-xs text-warning">No cardholders exist yet — create one first.</p>
          </FormField>

          <FormField label="Label">
            <input v-model="form.label" type="text" placeholder="Alice's badge" class="input input-bordered" />
          </FormField>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Valid from" hint="Leave blank for no bound.">
              <input v-model="form.valid_from" type="datetime-local" class="input input-bordered" />
            </FormField>
            <FormField label="Valid until" hint="Leave blank for no bound.">
              <input v-model="form.valid_until" type="datetime-local" class="input input-bordered" />
            </FormField>
          </div>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Credential</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
