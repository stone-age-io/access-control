<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import type PocketBase from 'pocketbase'
import { badgePb } from '@/utils/badgePb'
import QrCode from '@/components/ui/QrCode.vue'
import type { BadgeMe, BadgePassState } from '@/types/badge'

/**
 * The badge FACE: photo, name, QR, and validity.
 *
 * Everything shown comes from GET /api/badge/me (fetched by the parent) — this panel
 * never queries the policy collections, because the badge tier deliberately cannot read
 * them.
 *
 * # Why it takes a client
 *
 * `cardholders.photo` is a PROTECTED file, so its URL needs a short-lived file token, and
 * the token must be issued against whichever session is asking. The holder's own device
 * uses the badge client; the operator's read-only preview uses the operator one. Passing
 * the client in is what lets ONE component serve both — the alternative was a second
 * near-copy of this file, which is how the operator's badge modal and this panel had
 * already drifted into two versions of the same photo-loading code.
 *
 * The password form is deliberately NOT here any more: it lives in BadgePasswordModal,
 * reached from the account menu. It is a rare, deliberate act, and having it permanently
 * under the QR was most of the reason a badge did not fit a phone screen.
 */
const props = withDefaults(
  defineProps<{
    me: BadgeMe
    /** Which session's file token to build the photo URL with. */
    client?: PocketBase
    /** Trims the chrome for an embedded/preview context: smaller QR, no holder advice. */
    compact?: boolean
  }>(),
  { client: () => badgePb, compact: false },
)

const photoUrl = ref('')

function dateLabel(raw?: string): string {
  if (!raw) return ''
  const d = new Date(raw)
  return isNaN(d.getTime()) ? '' : d.toLocaleString()
}
const validFromLabel = computed(() => dateLabel(props.me.validFrom))
const validUntilLabel = computed(() => dateLabel(props.me.validUntil))

/**
 * A live clock, but only while there is a deadline worth watching.
 *
 * "Valid until 26 Jul 2026, 18:00" is the wrong unit for the person who most needs it: a
 * visitor with a few hours left wants "expires in 2h 40m". So the absolute time stays (it
 * is what you tell reception) and the countdown sits under it.
 *
 * The interval only exists when there IS an expiry — a staff badge with an open-ended
 * credential must not run a timer for the years it hangs on a lanyard.
 */
const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined

function stopTicker() {
  if (ticker !== undefined) {
    clearInterval(ticker)
    ticker = undefined
  }
}

const expiresAt = computed(() => {
  if (props.me.passState !== 'valid' || !props.me.validUntil) return 0
  const t = new Date(props.me.validUntil).getTime()
  return isNaN(t) ? 0 : t
})

watch(
  expiresAt,
  (at) => {
    stopTicker()
    if (!at) return
    now.value = Date.now()
    // A minute is the finest unit shown, so a minute is how often it needs redrawing.
    ticker = setInterval(() => {
      now.value = Date.now()
    }, 30_000)
  },
  { immediate: true },
)
onUnmounted(stopTicker)

/**
 * "2h 40m" / "3 days" / '' when there is nothing to count down to.
 *
 * Days rather than hours past 48h: nobody reads "71h left", and a contractor's week-long
 * pass would otherwise show a number that changes constantly and means nothing.
 */
const remainingLabel = computed(() => {
  const at = expiresAt.value
  if (!at) return ''
  const ms = at - now.value
  if (ms <= 0) return 'Expiring now'
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 60) return `Expires in ${minutes} min`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) {
    const rem = minutes % 60
    return rem ? `Expires in ${hours}h ${rem}m` : `Expires in ${hours}h`
  }
  return `Expires in ${Math.floor(hours / 24)} days`
})

/** Under an hour is worth a colour: it is the window in which someone should act. */
const remainingUrgent = computed(() => {
  const at = expiresAt.value
  return !!at && at - now.value < 60 * 60_000
})

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
 * The cardholder photo is a PROTECTED file, so its URL needs a short-lived file token,
 * issued against whichever session is asking (see the `client` prop).
 */
async function loadPhoto() {
  const m = props.me
  if (!m.photoFile || !m.photoRecord) {
    photoUrl.value = ''
    return
  }
  try {
    const token = await props.client.files.getToken()
    photoUrl.value = props.client.files.getURL(
      { id: m.photoRecord, collectionId: 'cardholders', collectionName: 'cardholders' },
      m.photoFile,
      { token, thumb: '400x400' },
    )
  } catch {
    photoUrl.value = '' // fall back to no photo rather than a broken image
  }
}
watch(() => [props.me.photoRecord, props.me.photoFile], loadPhoto, { immediate: true })
</script>

<template>
  <div class="card bg-base-100 shadow-sm">
    <div class="card-body items-center text-center gap-3 p-4">
      <!-- Photo centred above the name rather than beside it, and half again as large.
           A badge is a thing someone holds up to be compared against their face, so the photo
           is the largest element after the code itself; the old 64px thumbnail in a left-hand
           row was sized like a list avatar. Centring it also stops the name and email being
           squeezed into a narrow column beside it — they get the full card width and a size
           that can be read at arm's length. -->
      <img
        v-if="photoUrl"
        :src="photoUrl"
        :alt="me.name"
        class="rounded-full object-cover bg-base-300 shrink-0"
        :class="compact ? 'h-20 w-20' : 'h-28 w-28'"
      />
      <div
        v-else
        class="rounded-full bg-base-300 flex items-center justify-center font-semibold shrink-0"
        :class="compact ? 'h-20 w-20 text-2xl' : 'h-28 w-28 text-4xl'"
      >
        {{ (me.name || '?').slice(0, 1).toUpperCase() }}
      </div>

      <div class="w-full min-w-0">
        <div class="truncate font-bold" :class="compact ? 'text-base' : 'text-lg'">
          {{ me.name || 'Cardholder' }}
        </div>
        <div class="text-sm text-base-content/60 truncate">{{ me.email }}</div>
        <div v-if="me.kind === 'visitor'" class="badge badge-outline badge-sm mt-1">Visitor</div>
      </div>

      <!-- Why this badge does not work, when it does not. Says nothing at all
           when the pass is valid. -->
      <div
        v-if="passNotice"
        class="alert py-2 text-sm w-full text-left"
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
        <QrCode :value="me.qr" :size="compact ? 148 : 208" />
        <p v-if="!compact" class="text-xs text-base-content/60">
          {{
            me.qrSecret
              ? 'This code opens doors — treat it like a key and do not share a screenshot.'
              : 'For identification. This code does not open doors.'
          }}
        </p>
      </template>

      <div v-if="passUsable && validUntilLabel" class="text-xs">
        <div
          v-if="remainingLabel"
          class="font-medium"
          :class="remainingUrgent ? 'text-warning' : 'text-base-content/70'"
        >
          {{ remainingLabel }}
        </div>
        <div class="text-base-content/50">Valid until {{ validUntilLabel }}</div>
      </div>
    </div>
  </div>
</template>
