<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { badgePb } from '@/utils/badgePb'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import QrCode from '@/components/ui/QrCode.vue'
import type { BadgeMe, BadgePassState, BadgePortal } from '@/types/badge'

/**
 * The badge itself: photo, name, QR code, validity, and the doors this person may
 * open remotely.
 *
 * Everything shown comes from GET /api/badge/me — the client never queries the
 * policy collections, because the badge tier deliberately cannot read them. The
 * server sends portal NAMES only; codes, relay indices, and area membership stay
 * server-side.
 */
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

const me = ref<BadgeMe | null>(null)
const loading = ref(true)
const loadError = ref('')
const photoUrl = ref('')

// Per-portal in-flight + result state, so one door's outcome never overwrites
// another's.
const unlocking = ref<Record<string, boolean>>({})
const results = ref<Record<string, { ok: boolean; message: string }>>({})

const remotePortals = computed<BadgePortal[]>(() => (me.value?.portals || []).filter((p) => p.remoteUnlock))
const viewOnlyPortals = computed<BadgePortal[]>(() => (me.value?.portals || []).filter((p) => !p.remoteUnlock))

function dateLabel(raw?: string): string {
  if (!raw) return ''
  const d = new Date(raw)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}
const validFromLabel = computed(() => dateLabel(me.value?.validFrom))
const validUntilLabel = computed(() => dateLabel(me.value?.validUntil))

/**
 * The pass banner: one message per server-decided state, and nothing at all when the
 * pass is valid. Deliberately a lookup rather than a chain of v-ifs over several
 * booleans — the bug this replaced was exactly that kind of chain telling someone who
 * had never been issued a credential that their pass was "not currently valid".
 *
 * `null` means "say nothing", which is only the valid case.
 */
const passNotice = computed<{ text: string; tone: 'warning' | 'error' } | null>(() => {
  const state: BadgePassState = me.value?.passState || 'none'
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

/** Only a valid pass may drive a door. Everything else is read-only. */
const passUsable = computed(() => me.value?.passState === 'valid')

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    me.value = await badgePb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
    await loadPhoto()
  } catch (err: any) {
    if (err?.status === 401) {
      badgeAuth.logout()
      router.push('/badge/login')
      return
    }
    loadError.value = 'Could not load your badge. Try again in a moment.'
  } finally {
    loading.value = false
  }
}

/**
 * The cardholder photo is a PROTECTED file, so its URL needs a short-lived file
 * token. Built here with the badge client (not the operator one) so the token is
 * issued against the badge session.
 */
async function loadPhoto() {
  const m = me.value
  if (!m?.photoFile || !m.photoRecord) {
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

async function unlock(portal: BadgePortal) {
  unlocking.value = { ...unlocking.value, [portal.id]: true }
  delete results.value[portal.id]
  try {
    await badgePb.send(`/api/badge/unlock/${portal.id}`, { method: 'POST' })
    results.value = { ...results.value, [portal.id]: { ok: true, message: 'Unlocked' } }
  } catch (err: any) {
    // The server returns the stable policy reason code; surface it, because
    // "outside schedule" and "credential expired" call for different actions.
    const reason = err?.response?.reason as string | undefined
    results.value = {
      ...results.value,
      [portal.id]: {
        ok: false,
        message:
          err?.status === 429
            ? 'Too many attempts — wait a moment'
            : reasonText(reason) || 'Not allowed right now',
      },
    }
  } finally {
    const next = { ...unlocking.value }
    delete next[portal.id]
    unlocking.value = next
  }
}

/**
 * Turn a decision reason code into something the holder can act on. Unmapped codes fall
 * through to the generic message rather than showing an internal string.
 *
 * The keys are the STABLE codes from internal/policy/policy.go — they are a public
 * contract, so this map has to spell them exactly. It previously invented its own
 * (`deny_schedule`, `deny_credential_expired`, `deny_posture_lockdown`), none of which
 * exist, so every denial fell through to "Not allowed right now". The two non-policy
 * codes below come from badgeapi's own pre-checks.
 */
function reasonText(reason?: string): string {
  switch (reason) {
    case 'deny_schedule_closed':
      return 'Not within your allowed hours'
    case 'deny_expired':
      return 'Your pass has expired'
    case 'deny_not_yet_valid':
      return 'Your pass has not started yet'
    case 'deny_revoked':
      return 'Your pass is no longer active'
    case 'deny_unknown_credential':
      return 'Your pass is not recognised — contact your host'
    case 'deny_no_access':
      return 'Your badge does not open this door'
    case 'deny_lockdown':
      return 'This door is in lockdown'
    case 'deny_point_disabled':
      return 'This door is out of service'
    case 'deny_unknown_point':
      return 'This door is no longer available'
    // badgeapi's own checks, ahead of the policy decision.
    case 'remote_unlock_not_allowed':
      return 'This door cannot be opened remotely'
    case 'no_credential':
      return 'No pass is issued to you'
    default:
      return ''
  }
}

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

async function signOut() {
  badgeAuth.logout()
  router.push('/badge/login')
}

onMounted(load)
</script>

<template>
  <div class="min-h-screen bg-base-200 p-4">
    <div class="max-w-sm mx-auto space-y-4">
      <!-- Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <BrandLogo :size="28" />
          <span class="text-sm font-medium">{{ branding.appName }}</span>
        </div>
        <button class="btn btn-ghost btn-xs" @click="signOut">Sign out</button>
      </div>

      <div v-if="loading" class="flex justify-center p-12">
        <span class="loading loading-spinner loading-lg"></span>
      </div>

      <div v-else-if="loadError" class="alert alert-error text-sm">
        <span>{{ loadError }}</span>
        <button class="btn btn-xs" @click="load">Retry</button>
      </div>

      <template v-else-if="me">
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

        <!-- Remote unlock -->
        <div v-if="remotePortals.length" class="card bg-base-100 shadow-sm">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Open a door</h2>
            <div v-for="p in remotePortals" :key="p.id" class="space-y-1">
              <button
                class="btn btn-primary w-full justify-between"
                :disabled="unlocking[p.id] || !passUsable"
                @click="unlock(p)"
              >
                <span class="text-left">
                  {{ p.name }}
                  <span v-if="p.location" class="block text-xs font-normal opacity-70">{{ p.location }}</span>
                </span>
                <span v-if="unlocking[p.id]" class="loading loading-spinner loading-sm"></span>
              </button>
              <p
                v-if="results[p.id]"
                class="text-xs px-1"
                :class="results[p.id].ok ? 'text-success' : 'text-error'"
              >
                {{ results[p.id].message }}
              </p>
            </div>
          </div>
        </div>

        <!-- Doors on the badge that are not remotely openable: shown so the holder
             knows their badge works there in person. -->
        <div v-if="viewOnlyPortals.length" class="card bg-base-100 shadow-sm">
          <div class="card-body gap-2">
            <h2 class="card-title text-base">Your other doors</h2>
            <p class="text-xs text-base-content/60">
              {{
                passUsable
                  ? 'Present your badge at these in person.'
                  : 'These doors are on your badge, but need a valid pass to open.'
              }}
            </p>
            <ul class="text-sm space-y-1">
              <li v-for="p in viewOnlyPortals" :key="p.id" class="flex justify-between gap-2">
                <span>{{ p.name }}</span>
                <span class="text-base-content/50 text-xs">{{ p.location }}</span>
              </li>
            </ul>
          </div>
        </div>

        <div v-if="!me.portals.length" class="text-center text-sm text-base-content/50 py-4">
          No doors are assigned to your badge.
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
      </template>
    </div>
  </div>
</template>
