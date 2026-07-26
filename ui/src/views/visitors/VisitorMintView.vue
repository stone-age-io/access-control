<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { isoToLocalInput } from '@/utils/format'
import type { Role } from '@/types/pocketbase'
import type { MintVisitorRequest, MintVisitorResponse } from '@/types/badge'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import QrCode from '@/components/ui/QrCode.vue'

/**
 * Mint a visitor pass: one POST that creates a cardholder and a time-bound credential in a
 * single transaction (POST /api/badge/visitors).
 *
 * Deliberately NOT a generic form over the two collections. The credential value
 * must come from the server's CSPRNG (it is a key to a building, and a
 * client-supplied value could be guessable or made to collide), and a half-created
 * visitor is worse than a clean failure — so the whole thing is one server call.
 *
 * Only roles flagged `visitor_preset` are offered. That is enforced server-side too;
 * this is just the picker.
 *
 * # The no-SMTP path
 *
 * A visitor's default route in is an emailed one-time code. On an install with no mail
 * configured that route does not exist — and neither does password reset — so a mint with
 * nothing else attached produced a pass its holder could never see, and the operator had to
 * go and edit the cardholder afterwards to rescue it.
 *
 * Hence the optional initial password, handed over at the desk and never mailed (mail is
 * stored indefinitely, forwarded, and synced to devices, so emailing a door-opening password
 * would outlive every control around it), and the QR of the badge LINK on the success card —
 * a link, not a token, so the visitor still has to prove the address or the password.
 */
const toast = useToast()

const roles = ref<Role[]>([])
const loadingRoles = ref(true)
const submitting = ref(false)
const errors = ref<Record<string, string>>({})
const result = ref<MintVisitorResponse | null>(null)
const copied = ref(false)

const form = ref({
  name: '',
  email: '',
  role: '',
  validFrom: '',
  validUntil: '',
  label: '',
  password: '',
})

const badgeLink = computed(() => `${window.location.origin}/login?as=badge`)

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
  // The same bound the server enforces, said here so it is caught on the field.
  if (form.value.password && form.value.password.length < 8) {
    e.password = 'Use at least 8 characters, or leave blank for an emailed code'
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
  copied.value = false
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
    if (form.value.password) body.password = form.value.password

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

/**
 * Copy the badge link. Falls back to selecting the field, because `navigator.clipboard` is
 * unavailable on a plain-HTTP install — which is exactly the sort of install that is also
 * handing links over at a desk.
 */
async function copyLink() {
  try {
    await navigator.clipboard.writeText(badgeLink.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    toast.info('Select the link and copy it manually.')
  }
}

function reset() {
  result.value = null
  copied.value = false
  form.value = {
    name: '',
    email: '',
    role: form.value.role,
    validFrom: '',
    validUntil: '',
    label: '',
    password: '',
  }
  // A fresh working-day default for the next guest, not the previous one's window.
  form.value.validUntil = defaultUntil()
}

/**
 * Now → end of the working day. Explicit rather than open-ended, since an unbounded
 * "visitor" is the thing this flow exists to avoid.
 */
function defaultUntil(): string {
  const end = new Date()
  end.setHours(18, 0, 0, 0)
  if (end.getTime() < Date.now()) end.setDate(end.getDate() + 1)
  return isoToLocalInput(end)
}

onMounted(() => {
  loadRoles()
  form.value.validUntil = defaultUntil()
})
</script>

<template>
  <form @submit.prevent="handleSubmit">
    <FormLayout title="New Visitor Pass" :breadcrumbs="[{ label: 'Visitors', to: '/visitors' }, { label: 'New' }]">
      <!-- Success -->
      <BaseCard v-if="result" title="Pass issued">
        <div class="space-y-4">
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
              No invite email was sent (mail is not configured, or the send failed). The pass is valid —
              <template v-if="result.passwordSet">
                give the visitor the link below and the password you set, for
                <strong>{{ result.email }}</strong>.
              </template>
              <template v-else>
                give the visitor the link below and tell them to sign in with
                <strong>{{ result.email }}</strong> — they will be sent a one-time code, which needs
                working mail. Consider setting an initial password instead.
              </template>
            </span>
          </div>

          <!-- The handover. A link and a QR of that same link — nothing sensitive is in
               either, so it is safe to show on a lobby screen: the visitor still has to
               prove the address or produce the password. -->
          <div class="flex flex-col sm:flex-row gap-4 sm:items-start">
            <div class="flex-1 space-y-2">
              <FormField label="Badge link" hint="Where the visitor signs in. Carries no code or token.">
                <div class="join">
                  <input :value="badgeLink" readonly class="input input-bordered join-item font-mono text-sm flex-1" />
                  <button type="button" class="btn btn-outline join-item" @click="copyLink">
                    {{ copied ? '✓ Copied' : 'Copy' }}
                  </button>
                </div>
              </FormField>
              <p v-if="result.passwordSet" class="text-xs text-base-content/60">
                Their password is the one you typed. It was not emailed — hand it over in person.
                They can change it from their badge.
              </p>
            </div>
            <div class="flex flex-col items-center gap-1 shrink-0">
              <QrCode :value="badgeLink" :size="120" />
              <span class="text-xs text-base-content/50">Scan to open</span>
            </div>
          </div>

          <div class="flex flex-wrap gap-2">
            <button type="button" class="btn btn-primary btn-sm" @click="reset">Issue another</button>
            <router-link :to="`/visitors/${result.cardholderId}`" class="btn btn-outline btn-sm">
              View visitor
            </router-link>
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

        <!-- Optional, and the thing that makes this flow work with no mail server at all. -->
        <BaseCard title="Sign-in">
          <FormField
            label="Initial password"
            :error="errors.password"
            hint="Optional. Leave blank and the visitor is emailed a one-time code instead — which needs working mail. Set one to hand over at the desk; it is never emailed, and they can change it from their badge."
          >
            <input
              v-model="form.password"
              type="text"
              autocomplete="off"
              placeholder="Leave blank for an emailed code"
              class="input input-bordered"
            />
          </FormField>
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
