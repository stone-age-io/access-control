<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Cardholder, Role } from '@/types/pocketbase'
import type { MintVisitorRequest, MintVisitorResponse } from '@/types/badge'
import FormField from '@/components/ui/FormField.vue'
import { isoToLocalInput } from '@/utils/format'

/**
 * Extend a visit by REISSUING it: a fresh pass with a new window, the previous code revoked.
 *
 * # Why reissue rather than edit the expiry
 *
 * Editing `valid_until` on a live credential is the tempting one-field change, and it is
 * wrong. A visitor's QR carries the credential VALUE — it is a working key, on a screen, for
 * hours — so it gets photographed, screenshotted, and forwarded as a matter of course.
 * Pushing that same value's expiry out silently re-arms every copy of it. A reissue mints a
 * new value from the server's CSPRNG and revokes the old one, so extending a visit costs the
 * visitor one refresh of their badge and costs everyone who has a copy of yesterday's code
 * everything.
 *
 * # Why it is the same endpoint as minting
 *
 * POST /api/badge/visitors already recognises a returning visitor by email and refreshes them
 * in place (`reused: true`) — same person, new pass, old code revoked, all in one transaction.
 * A separate "extend" route would be a second implementation of exactly that, with its own
 * chance of leaving a live code behind. So this is the mint form with the identity fields
 * fixed.
 *
 * The preset is offered, not fixed, because a second visit is genuinely allowed to be a
 * different visit — the contractor who came for the lobby last week is in the plant room
 * today. It defaults to what they hold now.
 */
const props = defineProps<{ open: boolean; cardholder: Cardholder; currentRoleId: string }>()
const emit = defineEmits<{ 'update:open': [boolean]; reissued: [] }>()

const toast = useToast()

const roles = ref<Role[]>([])
const loadingRoles = ref(true)
const submitting = ref(false)
const errors = ref<Record<string, string>>({})

const form = ref({ role: '', validUntil: '', label: '' })

/**
 * The default window: now → 18:00, tomorrow if that has already passed. Same rule as the mint
 * form — a bounded working day, because an unbounded "visitor" is what that flow exists to
 * avoid.
 */
function defaultUntil(): string {
  const end = new Date()
  end.setHours(18, 0, 0, 0)
  if (end.getTime() < Date.now()) end.setDate(end.getDate() + 1)
  return isoToLocalInput(end)
}

async function loadRoles() {
  loadingRoles.value = true
  try {
    roles.value = await pb.collection('roles').getFullList<Role>({
      filter: 'visitor_preset = true',
      sort: 'code',
    })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load visitor presets')
  } finally {
    loadingRoles.value = false
  }
}

// Reset on open, and prefill the preset they hold — but only if it is still offered to
// visitors. A preset that has since been withdrawn must be re-chosen rather than silently
// re-granted, since the server would refuse it anyway.
watch(
  () => props.open,
  async (open) => {
    if (!open) return
    errors.value = {}
    form.value = { role: '', validUntil: defaultUntil(), label: '' }
    if (!roles.value.length) await loadRoles()
    if (roles.value.some((r) => r.id === props.currentRoleId)) {
      form.value.role = props.currentRoleId
    } else if (roles.value.length === 1) {
      form.value.role = roles.value[0].id
    }
  },
)

const email = computed(() => props.cardholder.email || '')

function close() {
  if (submitting.value) return
  emit('update:open', false)
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.role) e.role = 'Choose what this visit may access'
  if (!form.value.validUntil) e.validUntil = 'A visitor pass must expire'
  else if (new Date(form.value.validUntil).getTime() <= Date.now()) {
    e.validUntil = 'Must be in the future'
  }
  errors.value = e
  return !Object.keys(e).length
}

async function submit() {
  if (!validate()) return
  submitting.value = true
  try {
    const body: MintVisitorRequest = {
      // Identity is fixed: this is the same person, matched server-side by email, which is
      // uniquely indexed on cardholders.
      name: props.cardholder.name || email.value,
      email: email.value,
      role: form.value.role,
      validUntil: new Date(form.value.validUntil).toISOString(),
      label: form.value.label.trim() || undefined,
    }
    const res = await pb.send<MintVisitorResponse>('/api/badge/visitors', { method: 'POST', body })
    toast.success(
      res.reused
        ? 'Pass reissued — their previous code is revoked'
        : 'Pass issued',
    )
    emit('reissued')
    emit('update:open', false)
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to reissue the pass')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal modal-open" role="dialog">
    <div class="modal-box max-w-md">
      <h3 class="font-bold text-lg">Reissue this pass</h3>
      <p class="text-sm text-base-content/60 mt-1">
        A new pass for {{ cardholder.name || email }}, with a new code.
      </p>

      <div class="alert alert-warning py-2 text-sm mt-3">
        <span>
          Their current code stops working immediately. Their badge shows the new one as soon
          as they refresh it — nothing is emailed.
        </span>
      </div>

      <form class="space-y-4 mt-4" @submit.prevent="submit">
        <div v-if="loadingRoles" class="flex justify-center p-4">
          <span class="loading loading-spinner"></span>
        </div>
        <div v-else-if="!roles.length" class="alert alert-warning text-sm">
          <span>
            No visitor presets are configured. Edit a role and turn on
            <strong>Offer to visitors</strong> to make it selectable here.
          </span>
        </div>
        <FormField
          v-else
          label="Access preset"
          required
          :error="errors.role"
          hint="What this visit may open. Replaces what they hold now."
        >
          <select v-model="form.role" class="select select-bordered select-sm">
            <option value="">Choose…</option>
            <option v-for="r in roles" :key="r.id" :value="r.id">{{ r.name || r.code }}</option>
          </select>
        </FormField>

        <FormField
          label="Valid until"
          required
          :error="errors.validUntil"
          hint="Enforced at the door, not just here. Maximum 30 days."
        >
          <input v-model="form.validUntil" type="datetime-local" class="input input-bordered input-sm" />
        </FormField>

        <FormField label="Note" hint="Optional label on the new pass (e.g. 'Acme HVAC, day 2').">
          <input v-model="form.label" type="text" class="input input-bordered input-sm" />
        </FormField>

        <div class="modal-action">
          <button type="button" class="btn btn-sm btn-ghost" :disabled="submitting" @click="close">
            Cancel
          </button>
          <button type="submit" class="btn btn-sm btn-primary" :disabled="submitting || !roles.length">
            <span v-if="submitting" class="loading loading-spinner loading-sm"></span>
            <span v-else>Reissue pass</span>
          </button>
        </div>
      </form>
    </div>
    <div class="modal-backdrop bg-black/40" @click="close"></div>
  </div>
</template>
