<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { badgePb } from '@/utils/badgePb'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import QrCode from '@/components/ui/QrCode.vue'
import type { BadgeMe, BadgePassState } from '@/types/badge'

/**
 * The badge FACE: photo, name, QR, validity, and the holder's own password.
 *
 * Everything shown comes from GET /api/badge/me (fetched by the parent) — this panel
 * never queries the policy collections, because the badge tier deliberately cannot read
 * them. Split from the shell so the Access tab can grow a floorplan without this file
 * growing with it.
 */
const props = defineProps<{ me: BadgeMe }>()

const badgeAuth = useBadgeAuthStore()
const photoUrl = ref('')

function dateLabel(raw?: string): string {
  if (!raw) return ''
  const d = new Date(raw)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}
const validFromLabel = computed(() => dateLabel(props.me.validFrom))
const validUntilLabel = computed(() => dateLabel(props.me.validUntil))

/**
 * The pass banner: one message per server-decided state, and nothing at all when the
 * pass is valid. Deliberately a lookup rather than a chain of v-ifs over several
 * booleans — the bug this replaced was exactly that kind of chain telling someone who
 * had never been issued a credential that their pass was "not currently valid".
 *
 * `null` means "say nothing", which is only the valid case.
 */
const passNotice = computed<{ text: string; tone: 'warning' | 'error' } | null>(() => {
  const state: BadgePassState = props.me.passState || 'none'
  switch (state) {
    case 'valid':
      return null
    case 'none':
      return {
        text: 'No pass has been issued to you yet. Contact your host or reception.',
        tone: 'warning',
      }
    case 'not_yet_valid':
      return {
        text: validFromLabel.value
          ? `Your pass starts ${validFromLabel.value}.`
          : 'Your pass has not started yet.',
        tone: 'warning',
      }
    case 'expired':
      return {
        text: validUntilLabel.value
          ? `Your pass expired ${validUntilLabel.value}. Contact your host to renew it.`
          : 'Your pass has expired. Contact your host to renew it.',
        tone: 'warning',
      }
    case 'suspended':
      return { text: 'Your badge is suspended. Contact your host.', tone: 'error' }
  }
})

const passUsable = computed(() => props.me.passState === 'valid')

/**
 * The cardholder photo is a PROTECTED file, so its URL needs a short-lived file token.
 * Built with the badge client (not the operator one) so the token is issued against the
 * badge session.
 */
async function loadPhoto() {
  const m = props.me
  if (!m.photoFile || !m.photoRecord) {
    photoUrl.value = ''
    return
  }
  try {
    const token = await badgePb.files.getToken()
    photoUrl.value = badgePb.files.getURL(
      { id: m.photoRecord, collectionId: 'cardholders', collectionName: 'cardholders' },
      m.photoFile,
      { token, thumb: '400x400' },
    )
  } catch {
    photoUrl.value = '' // fall back to no photo rather than a broken image
  }
}
watch(() => [props.me.photoRecord, props.me.photoFile], loadPhoto, { immediate: true })

// --- password management ---
//
// A holder who signed in by OTP has no password yet, so this is "Set a password"; one
// who has is "Change password" and must supply the current one. The server re-reads
// `password_set` and is the real gate — this only shapes the form.
const showPasswordForm = ref(false)
const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const savingPassword = ref(false)
const passwordError = ref('')
const passwordSaved = ref(false)

function togglePasswordForm() {
  showPasswordForm.value = !showPasswordForm.value
  oldPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
  passwordError.value = ''
  passwordSaved.value = false
}

async function savePassword() {
  passwordError.value = ''
  passwordSaved.value = false
  if (newPassword.value.length < 8) {
    passwordError.value = 'Use at least 8 characters.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = 'The two passwords do not match.'
    return
  }
  savingPassword.value = true
  try {
    // The store re-authenticates afterwards: changing a password rotates the record's
    // token key server-side, which invalidates this very session.
    await badgeAuth.setPassword(newPassword.value, confirmPassword.value, oldPassword.value)
    oldPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    // Collapse the form so the confirmation below it is what the holder sees.
    showPasswordForm.value = false
    passwordSaved.value = true
  } catch (err: any) {
    passwordError.value =
      err?.status === 429
        ? 'Too many attempts. Wait a minute and try again.'
        : err?.response?.message || 'Could not set your password.'
  } finally {
    savingPassword.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <!-- The badge -->
    <div class="card bg-base-100 shadow-sm">
      <div class="card-body items-center text-center gap-3">
        <img
          v-if="photoUrl"
          :src="photoUrl"
          :alt="me.name"
          class="w-24 h-24 rounded-full object-cover bg-base-300"
        />
        <div
          v-else
          class="w-24 h-24 rounded-full bg-base-300 flex items-center justify-center text-2xl font-semibold"
        >
          {{ (me.name || '?').slice(0, 1).toUpperCase() }}
        </div>

        <div>
          <div class="font-bold text-lg">{{ me.name || 'Cardholder' }}</div>
          <div class="text-sm text-base-content/60">{{ me.email }}</div>
        </div>

        <div v-if="me.kind === 'visitor'" class="badge badge-outline">Visitor</div>

        <!-- Why this badge does not work, when it does not. Says nothing at all
             when the pass is valid. -->
        <div
          v-if="passNotice"
          class="alert py-2 text-sm w-full"
          :class="passNotice.tone === 'error' ? 'alert-error' : 'alert-warning'"
        >
          <span>{{ passNotice.text }}</span>
        </div>

        <!-- The QR is whatever the server sent: the credential value for a live
             visitor pass, an inert identifier for a staff badge, nothing at all for
             a suspended one. A staff badge keeps showing its identifier while a
             credential is pending — it identifies the person, which is true
             regardless, and it says so below. -->
        <template v-if="me.qr">
          <QrCode :value="me.qr" :size="200" />
          <p v-if="me.qrSecret" class="text-xs text-base-content/60">
            This code opens doors — treat it like a key and do not share a screenshot.
          </p>
          <p v-else class="text-xs text-base-content/60">
            For identification. This code does not open doors.
          </p>
        </template>

        <div v-if="passUsable && validUntilLabel" class="text-xs text-base-content/50">
          Valid until {{ validUntilLabel }}
        </div>
      </div>
    </div>

    <!-- Password: set a first one (signed in by code) or change an existing one. -->
    <div class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3">
        <div class="flex items-center justify-between gap-2">
          <div class="min-w-0">
            <h2 class="card-title text-base">
              {{ badgeAuth.hasPassword ? 'Change password' : 'Set a password' }}
            </h2>
            <p class="text-xs text-base-content/60">
              {{
                badgeAuth.hasPassword
                  ? 'Sign in without waiting for an emailed code.'
                  : 'Optional. Lets you sign in without waiting for an emailed code.'
              }}
            </p>
          </div>
          <button class="btn btn-sm btn-outline shrink-0" @click="togglePasswordForm">
            {{ showPasswordForm ? 'Cancel' : badgeAuth.hasPassword ? 'Change' : 'Set' }}
          </button>
        </div>

        <form v-if="showPasswordForm" class="space-y-3" @submit.prevent="savePassword">
          <div v-if="passwordError" class="alert alert-error py-2 text-sm">{{ passwordError }}</div>

          <label v-if="badgeAuth.hasPassword" class="form-control">
            <span class="label-text mb-1">Current password</span>
            <input
              v-model="oldPassword"
              type="password"
              autocomplete="current-password"
              class="input input-bordered input-sm"
              :disabled="savingPassword"
            />
          </label>
          <label class="form-control">
            <span class="label-text mb-1">New password</span>
            <input
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              class="input input-bordered input-sm"
              :disabled="savingPassword"
            />
          </label>
          <label class="form-control">
            <span class="label-text mb-1">Confirm new password</span>
            <input
              v-model="confirmPassword"
              type="password"
              autocomplete="new-password"
              class="input input-bordered input-sm"
              :disabled="savingPassword"
            />
          </label>
          <p class="text-xs text-base-content/50">
            At least 8 characters. Setting a password signs out your other devices.
          </p>
          <button type="submit" class="btn btn-primary btn-sm w-full" :disabled="savingPassword">
            <span v-if="savingPassword" class="loading loading-spinner loading-sm"></span>
            <span v-else>Save password</span>
          </button>
        </form>

        <p v-else-if="passwordSaved" class="text-sm text-success">Your password has been set.</p>
      </div>
    </div>
  </div>
</template>
