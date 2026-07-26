<script setup lang="ts">
import { ref, watch } from 'vue'
import { useBadgeAuthStore } from '@/stores/badgeAuth'

/**
 * A holder setting or changing their own password.
 *
 * # Why a modal, and why it moved here
 *
 * It used to be an always-present card on the Badge tab, below the QR. That put a form
 * almost nobody uses — most holders set a password once, or never, and sign in with an
 * emailed code — permanently in front of the thing everybody uses, and it was most of the
 * reason the badge did not fit a phone screen. It is a rare, deliberate act, so it belongs
 * behind a menu.
 *
 * # Set vs change
 *
 * A holder who signed in by OTP has no password yet, so this is "Set a password" and there
 * is nothing to prove. One who has must supply the current one — otherwise a stolen
 * session could lock them out of their own badge. `password_set` decides which, and the
 * server re-reads it and is the real gate; this only shapes the form.
 */
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; saved: [] }>()

const badgeAuth = useBadgeAuthStore()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const saving = ref(false)
const error = ref('')

// Reset on every open rather than on close: a half-typed password must not be sitting
// there the next time the modal appears, and clearing on close would blank the fields
// while the dialog is still fading out.
watch(
  () => props.open,
  (open) => {
    if (!open) return
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    error.value = ''
  },
)

function close() {
  if (saving.value) return // a password change is mid-flight; losing the form now is worse
  emit('update:open', false)
}

async function save() {
  error.value = ''
  if (newPassword.value.length < 8) {
    error.value = 'Use at least 8 characters.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = 'The two passwords do not match.'
    return
  }
  saving.value = true
  try {
    // The store re-authenticates afterwards: changing a password rotates the record's
    // token key server-side, which invalidates this very session.
    await badgeAuth.setPassword(newPassword.value, confirmPassword.value, oldPassword.value)
    emit('saved')
    emit('update:open', false)
  } catch (err: any) {
    error.value =
      err?.status === 429
        ? 'Too many attempts. Wait a minute and try again.'
        : err?.response?.message || 'Could not set your password.'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div v-if="open" class="modal modal-open" role="dialog">
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-lg">
        {{ badgeAuth.hasPassword ? 'Change your password' : 'Set a password' }}
      </h3>
      <p class="text-xs text-base-content/60 mt-1">
        {{
          badgeAuth.hasPassword
            ? 'Sign in without waiting for an emailed code.'
            : 'Optional. Lets you sign in without waiting for an emailed code.'
        }}
      </p>

      <form class="space-y-3 mt-4" @submit.prevent="save">
        <div v-if="error" class="alert alert-error py-2 text-sm">{{ error }}</div>

        <label v-if="badgeAuth.hasPassword" class="form-control">
          <span class="label-text mb-1">Current password</span>
          <input
            v-model="oldPassword"
            type="password"
            autocomplete="current-password"
            class="input input-bordered input-sm"
            :disabled="saving"
          />
        </label>
        <label class="form-control">
          <span class="label-text mb-1">New password</span>
          <input
            v-model="newPassword"
            type="password"
            autocomplete="new-password"
            class="input input-bordered input-sm"
            :disabled="saving"
          />
        </label>
        <label class="form-control">
          <span class="label-text mb-1">Confirm new password</span>
          <input
            v-model="confirmPassword"
            type="password"
            autocomplete="new-password"
            class="input input-bordered input-sm"
            :disabled="saving"
          />
        </label>
        <p class="text-xs text-base-content/50">
          At least 8 characters. Setting a password signs out your other devices.
        </p>

        <div class="modal-action">
          <button type="button" class="btn btn-sm btn-ghost" :disabled="saving" @click="close">
            Cancel
          </button>
          <button type="submit" class="btn btn-sm btn-primary" :disabled="saving">
            <span v-if="saving" class="loading loading-spinner loading-sm"></span>
            <span v-else>Save password</span>
          </button>
        </div>
      </form>
    </div>
    <div class="modal-backdrop bg-black/40" @click="close"></div>
  </div>
</template>
