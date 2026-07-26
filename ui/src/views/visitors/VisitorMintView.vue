<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Role } from '@/types/pocketbase'
import type { MintVisitorRequest, MintVisitorResponse } from '@/types/badge'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

/**
 * Mint a visitor pass: one POST that creates a cardholder, a time-bound credential,
 * and a `visitor` badge login in a single transaction (POST /api/badge/visitors).
 *
 * Deliberately NOT a generic form over the three collections. The credential value
 * must come from the server's CSPRNG (it is a key to a building, and a
 * client-supplied value could be guessable or made to collide), and a half-created
 * visitor is worse than a clean failure — so the whole thing is one server call.
 *
 * Only roles flagged `visitor_preset` are offered. That is enforced server-side too;
 * this is just the picker.
 */
const toast = useToast()

const roles = ref<Role[]>([])
const loadingRoles = ref(true)
const submitting = ref(false)
const errors = ref<Record<string, string>>({})
const result = ref<MintVisitorResponse | null>(null)

const form = ref({
  name: '',
  email: '',
  role: '',
  validFrom: '',
  validUntil: '',
  label: '',
})

const badgeLink = computed(() => `${window.location.origin}/badge`)

/** `datetime-local` wants "YYYY-MM-DDTHH:mm" in LOCAL time. */
function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function loadRoles() {
  loadingRoles.value = true
  try {
    roles.value = await pb.collection('roles').getFullList<Role>({
      filter: 'visitor_preset = true',
      sort: 'code',
    })
    if (roles.value.length === 1) form.value.role = roles.value[0].id
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load visitor roles')
  } finally {
    loadingRoles.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.name.trim()) e.name = 'Name is required'
  if (!form.value.email.trim()) e.email = 'Email is required — it is how the visitor receives their sign-in code'
  if (!form.value.role) e.role = 'Choose what the visitor may access'
  if (!form.value.validUntil) e.validUntil = 'A visitor pass must expire'
  if (form.value.validFrom && form.value.validUntil && form.value.validUntil <= form.value.validFrom) {
    e.validUntil = 'Must be after the start time'
  }
  errors.value = e
  const first = Object.values(e)[0]
  if (first) toast.error(first)
  return !first
}

async function handleSubmit() {
  if (!validate()) return
  submitting.value = true
  result.value = null
  try {
    // datetime-local values are local wall-clock; toISOString normalizes to the
    // RFC 3339 UTC the API expects.
    const body: MintVisitorRequest = {
      name: form.value.name.trim(),
      email: form.value.email.trim(),
      role: form.value.role,
      validUntil: new Date(form.value.validUntil).toISOString(),
      label: form.value.label.trim() || undefined,
    }
    if (form.value.validFrom) body.validFrom = new Date(form.value.validFrom).toISOString()

    result.value = await pb.send<MintVisitorResponse>('/api/badge/visitors', {
      method: 'POST',
      body,
    })
    toast.success(result.value.reused ? 'Visitor pass reissued' : 'Visitor pass created')
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to mint visitor pass')
  } finally {
    submitting.value = false
  }
}

function reset() {
  result.value = null
  form.value = { name: '', email: '', role: form.value.role, validFrom: '', validUntil: '', label: '' }
}

onMounted(() => {
  loadRoles()
  // Default window: now → end of the working day. Explicit rather than open-ended,
  // since an unbounded "visitor" is the thing this flow exists to avoid.
  const end = new Date()
  end.setHours(18, 0, 0, 0)
  if (end.getTime() < Date.now()) end.setDate(end.getDate() + 1)
  form.value.validUntil = toLocalInput(end)
})
</script>

<template>
  <form @submit.prevent="handleSubmit">
    <FormLayout title="New Visitor Pass" :breadcrumbs="[{ label: 'Visitors', to: '/visitors' }, { label: 'New' }]">
      <!-- Success -->
      <BaseCard v-if="result" title="Pass issued">
        <div class="space-y-3">
          <p v-if="result.reused" class="alert alert-info py-2 text-sm">
            This person already had a visitor pass — it was refreshed and their previous code revoked.
          </p>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
            <div>
              <div class="text-base-content/60 text-xs uppercase">Visitor</div>
              <div class="font-medium">{{ result.email }}</div>
            </div>
            <div>
              <div class="text-base-content/60 text-xs uppercase">Valid until</div>
              <div class="font-medium">{{ new Date(result.validUntil).toLocaleString() }}</div>
            </div>
          </div>

          <div v-if="result.inviteSent" class="alert alert-success py-2 text-sm">
            An invite email was sent with sign-in instructions.
          </div>
          <div v-else class="alert alert-warning py-2 text-sm">
            <span>
              No invite email was sent (mail is not configured, or the send failed). The pass is valid — give
              the visitor the link below and tell them to sign in with <strong>{{ result.email }}</strong>.
            </span>
          </div>

          <FormField label="Badge link" hint="The visitor enters their email here and is sent a one-time code.">
            <input :value="badgeLink" readonly class="input input-bordered font-mono text-sm" />
          </FormField>

          <div class="flex gap-2">
            <button type="button" class="btn btn-primary btn-sm" @click="reset">Issue another</button>
            <router-link :to="`/cardholders/${result.cardholderId}`" class="btn btn-ghost btn-sm">
              View cardholder
            </router-link>
          </div>
        </div>
      </BaseCard>

      <!-- Form -->
      <template v-else>
        <BaseCard title="Visitor">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Name" required :error="errors.name">
              <input v-model="form.name" type="text" placeholder="Jane Guest" class="input input-bordered" />
            </FormField>
            <FormField label="Email" required :error="errors.email"
                       hint="Their sign-in code is emailed here. Nothing is stored in a link.">
              <input v-model="form.email" type="email" placeholder="jane@example.com" class="input input-bordered" />
            </FormField>
          </div>
        </BaseCard>

        <BaseCard title="Access">
          <div v-if="loadingRoles" class="flex justify-center p-4">
            <span class="loading loading-spinner"></span>
          </div>
          <div v-else-if="!roles.length" class="alert alert-warning text-sm">
            <span>
              No visitor roles are configured. Edit a role and turn on <strong>Offer to visitors</strong> to make it
              selectable here.
            </span>
          </div>
          <div v-else class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Access preset" required :error="errors.role"
                       hint="What this visitor may open, for this visit only.">
              <select v-model="form.role" class="select select-bordered">
                <option value="">Choose…</option>
                <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name || r.code }}</option>
              </select>
            </FormField>
            <FormField label="Note" hint="Optional label on the credential (e.g. 'Acme HVAC service').">
              <input v-model="form.label" type="text" class="input input-bordered" />
            </FormField>
          </div>
        </BaseCard>

        <BaseCard title="Validity">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Valid from" hint="Leave blank to start immediately.">
              <input v-model="form.validFrom" type="datetime-local" class="input input-bordered" />
            </FormField>
            <FormField label="Valid until" required :error="errors.validUntil"
                       hint="Enforced at the door, not just here — the pass stops working at this time even if the controller is offline. Maximum 30 days.">
              <input v-model="form.validUntil" type="datetime-local" class="input input-bordered" />
            </FormField>
          </div>
        </BaseCard>
      </template>

      <!-- A named slot must be a DIRECT child of FormLayout, so the result/form
           branch is expressed inside it rather than around it. -->
      <template #actions>
        <template v-if="!result">
          <router-link to="/visitors" class="btn btn-ghost">Cancel</router-link>
          <button type="submit" class="btn btn-primary" :disabled="submitting || !roles.length">
            <span v-if="submitting" class="loading loading-spinner"></span>
            <span v-else>Issue pass</span>
          </button>
        </template>
      </template>
    </FormLayout>
  </form>
</template>
