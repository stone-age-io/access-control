<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useFileUrl } from '@/composables/useFileUrl'
import { useAuthStore } from '@/stores/auth'
import { policyKey } from '@/utils/policyKey'
import type { Cardholder, Credential, Role } from '@/types/pocketbase'
import type { BadgeInviteResponse } from '@/types/badge'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import Avatar from '@/components/ui/Avatar.vue'
import BadgePreviewModal from '@/components/common/BadgePreviewModal.vue'
import VisitorReissueModal from './VisitorReissueModal.vue'
import { visitorPassState } from './visitorState'
import type { SoftTone } from '@/utils/badges'

/**
 * One visit, in full.
 *
 * # Why this page exists separately from the cardholder detail
 *
 * A visitor IS a cardholder (`kind = "visitor"`), and the cardholder page already shows the
 * policy graph around a person. But a visit is a different object of attention: it has a
 * window that closes, it is ended rather than edited, and the operator looking at it is
 * almost always answering "is this person's pass working right now, and if not why". So this
 * page leads with the pass and its state, and puts the four things you actually do to a
 * visit — see their badge, mail them the link again, reissue, revoke — in the header.
 *
 * The mint flow could only ever hand back an id and a link; there was nowhere to go and look
 * at what it had created.
 *
 * # The one deliberate absence
 *
 * No edit form. Everything editable about a visitor is either identity (which lives on the
 * cardholder form, linked below) or the pass window — and the pass is not edited, it is
 * REISSUED: a new credential from the server's CSPRNG, with the previous visit's code
 * revoked. Editing `valid_until` on a live credential would silently extend a code that has
 * already been photographed, screenshotted, and possibly forwarded.
 */
const route = useRoute()
const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
// cardholders.photo is a protected file — its URL needs a session file token.
const { url: fileUrl } = useFileUrl()
const auth = useAuthStore()

const recordId = route.params.id as string
const record = ref<Cardholder | null>(null)
const credentials = ref<Credential[]>([])
const loading = ref(true)
const working = ref(false)
const showBadgePreview = ref(false)
const showReissue = ref(false)

const canManage = computed(() => auth.can('enroll'))
const roles = computed<Role[]>(() => record.value?.expand?.roles || [])
const title = computed(() => record.value?.name || record.value?.email || 'Visitor')
const kvKey = computed(() => (record.value ? policyKey('cardholders', record.value) : ''))

/**
 * The pass, as one state. Derived from the CREDENTIAL rather than the person, because the
 * credential is what the edge enforces — `policy.Decide` reads its window offline, so
 * anything this page said that disagreed with it would be a lie the doors would expose.
 *
 * Shared with the list page so the two cannot describe the same visit differently.
 */
const pass = computed(() => visitorPassState(record.value, credentials.value))

/** The credential whose window the state describes, for the dates below. */
const current = computed<Credential | null>(() => pass.value.credential)

function dateLabel(raw?: string): string {
  if (!raw) return '—'
  const d = new Date(raw)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

/**
 * The sign-in methods actually available. `password_set` is what distinguishes a password
 * the visitor was given from the random fill every auth record carries — and on an install
 * with no SMTP it is the difference between a usable pass and one nobody can open, since
 * OTP and password reset are both emails.
 */
const signInMethods = computed<string[]>(() => {
  if (!record.value?.badge_login) return []
  return record.value.password_set ? ['Password', 'Emailed code'] : ['Emailed code']
})

async function load() {
  loading.value = true
  try {
    const [ch, creds] = await Promise.all([
      pb.collection('cardholders').getOne<Cardholder>(recordId, { expand: 'roles' }),
      pb.collection('credentials').getFullList<Credential>({
        filter: `user = "${recordId}"`,
        sort: '-created',
      }),
    ])
    record.value = ch
    credentials.value = creds
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load visitor')
    router.push('/visitors')
  } finally {
    loading.value = false
  }
}

/**
 * End the visit: revokes the credentials, keeps the person. The primary action, because an
 * operator reaching for a button here almost always means "stop them getting in", not
 * "erase the visit".
 */
async function revoke() {
  const ch = record.value
  if (!ch) return
  const ok = await confirm({
    title: 'Revoke this pass?',
    message: `End the visit for ${ch.name || ch.email}?`,
    details:
      'Their pass stops opening doors immediately, including their QR code. They keep their sign-in, and their badge will say the pass is no longer valid.',
    confirmText: 'Revoke pass',
    variant: 'warning',
  })
  if (!ok) return
  working.value = true
  try {
    await pb.send(`/api/badge/visitors/${ch.id}/revoke`, { method: 'POST' })
    toast.success('Visitor pass revoked')
    await load()
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to revoke the pass')
  } finally {
    working.value = false
  }
}

/**
 * Remove the person entirely. `credentials.user` cascades (migration 1750000036), so this
 * cannot leave a working credential behind — but it does destroy the record that they were
 * here, which is why it stays the quiet, last action.
 */
async function remove() {
  const ch = record.value
  if (!ch) return
  const ok = await confirm({
    title: 'Delete this visitor?',
    message: `Delete ${ch.name || ch.email}?`,
    details:
      'Their pass and their sign-in are deleted with them, and the record that they visited is gone. Revoke instead if they may return.',
    confirmText: 'Delete visitor',
    variant: 'danger',
  })
  if (!ok) return
  working.value = true
  try {
    await pb.collection('cardholders').delete(ch.id)
    toast.success('Visitor deleted')
    router.push('/visitors')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete the visitor')
  } finally {
    working.value = false
  }
}

/**
 * Mail them where their badge is, again. A separate act from issuing the pass — a visit
 * booked for next week is minted now and mailed when it suits, and a resend is what covers
 * the mail that went to spam.
 */
async function resendInvite() {
  const ch = record.value
  if (!ch) return
  working.value = true
  try {
    const res = await pb.send<BadgeInviteResponse>(`/api/badge/invite/${ch.id}`, { method: 'POST' })
    if (res.sent) toast.success(`Invite sent to ${res.email}`)
    else
      toast.warning(
        'The invite could not be sent (mail is not configured, or the send failed). Give them the sign-in details in person.',
      )
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to send the invite')
  } finally {
    working.value = false
  }
}

function credTone(status: string): SoftTone {
  if (status === 'active') return 'success'
  if (status === 'revoked') return 'error'
  return 'warning'
}

const credentialSearch = (c: Credential) => [c.value, c.label, c.type].filter(Boolean).join(' ')

onMounted(load)
</script>

<template>
  <div v-if="loading" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <DetailLayout
    v-else-if="record"
    :title="title"
    :breadcrumbs="[{ label: 'Visitors', to: '/visitors' }, { label: title }]"
  >
    <template #actions>
      <!-- Read-only, and the first thing an operator wants when a visitor says their pass
           does not work: what their own screen says. See BadgePreviewModal. -->
      <button v-if="canManage" class="btn btn-sm btn-primary" @click="showBadgePreview = true">
        View their badge
      </button>
      <button
        v-if="canManage && record.badge_login"
        class="btn btn-sm btn-ghost"
        :disabled="working"
        @click="resendInvite"
      >
        Resend invite
      </button>
      <button v-if="canManage" class="btn btn-sm btn-ghost" :disabled="working" @click="showReissue = true">
        Reissue
      </button>
      <button
        v-if="canManage && pass.label === 'active'"
        class="btn btn-sm btn-ghost text-warning"
        :disabled="working"
        @click="revoke"
      >
        Revoke
      </button>
      <button v-if="canManage" class="btn btn-sm btn-ghost text-error" :disabled="working" @click="remove">
        Delete
      </button>
    </template>

    <!-- The visit -->
    <BaseCard>
      <div class="flex items-center gap-3 mb-5">
        <Avatar
          :name="record.name || record.email"
          :seed="record.id"
          size="md"
          :src="fileUrl(record, record.photo, '400x400')"
        />
        <div class="min-w-0">
          <div class="font-bold truncate">{{ record.name || 'Unnamed' }}</div>
          <div class="text-sm text-base-content/60 truncate">{{ record.email || '—' }}</div>
        </div>
        <SoftBadge class="ml-auto shrink-0" :tone="pass.tone" dot>{{ pass.label }}</SoftBadge>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Valid from">{{ dateLabel(current?.valid_from) }}</DataField>
        <DataField label="Valid until">{{ dateLabel(current?.valid_until) }}</DataField>
        <DataField label="Cardholder status">
          <SoftBadge :tone="record.status === 'suspended' ? 'warning' : 'success'" dot>
            {{ record.status || 'active' }}
          </SoftBadge>
        </DataField>
        <DataField label="Sign-in">
          <span v-if="signInMethods.length" class="flex flex-wrap items-center gap-1">
            <SoftBadge v-for="m in signInMethods" :key="m" :tone="m === 'Password' ? 'success' : 'neutral'">
              {{ m }}
            </SoftBadge>
          </span>
          <span v-else class="opacity-40">No badge login</span>
        </DataField>
        <DataField label="Note">
          <span v-if="current?.label">{{ current.label }}</span>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <!-- The same row, through the operator lens: everything about this person that is
             not about the visit. -->
        <DataField label="Cardholder record">
          <router-link :to="`/cardholders/${record.id}`" class="link link-primary">
            Open cardholder
          </router-link>
        </DataField>
      </div>

      <!-- The state that produces a confusing badge: they can sign in and be told,
           correctly but unhelpfully, that no pass is issued. -->
      <div v-if="record.badge_login && !credentials.length" class="alert alert-warning py-2 text-sm mt-4">
        <span>
          This visitor has a sign-in but no pass, so their badge will say "no pass has been
          issued". Reissue to give them one.
        </span>
      </div>
      <div v-else-if="!record.badge_login" class="alert alert-warning py-2 text-sm mt-4">
        <span>
          This visitor has no badge login, so they cannot sign in to see their pass at all.
          Turn on <strong>Badge login</strong> on their cardholder record.
        </span>
      </div>
    </BaseCard>

    <!-- Every pass this person has held, this visit and previous ones. The history IS the
         reason revoke keeps the record. -->
    <RelationList
      title="Passes"
      icon="🎫"
      :items="credentials"
      :to="(c) => `/credentials/${c.id}`"
      :search-text="credentialSearch"
      hint="Newest first. A reissue revokes the previous code rather than extending it."
      empty="No pass has been issued for this visit."
    >
      <template #item="{ item: cred }">
        <code class="text-sm font-medium text-primary truncate">{{ cred.value }}</code>
        <span v-if="cred.label" class="text-sm opacity-60 truncate flex-1">{{ cred.label }}</span>
        <span class="text-xs opacity-50 shrink-0">{{ dateLabel(cred.valid_until) }}</span>
        <SoftBadge class="ml-auto" :tone="credTone(cred.status || '')" dot>
          {{ cred.status || 'active' }}
        </SoftBadge>
      </template>
    </RelationList>

    <!-- What the visit grants. One preset, replaced on each reissue. -->
    <RelationList
      title="Access preset"
      icon="🛡️"
      :items="roles"
      :to="(r) => `/roles/${r.id}`"
      :primary="(r) => r.code"
      :secondary="(r) => r.name"
      hint="A visit grants exactly the preset chosen when the pass was issued."
      empty="No preset assigned, so this pass opens nothing."
    />

    <RecordMeta :record="record" :kv-key="kvKey" />

    <BadgePreviewModal
      v-model:open="showBadgePreview"
      :cardholder-id="record.id"
      :name="title"
    />
    <VisitorReissueModal
      v-model:open="showReissue"
      :cardholder="record"
      :current-role-id="roles[0]?.id || ''"
      @reissued="load"
    />
  </DetailLayout>
</template>
