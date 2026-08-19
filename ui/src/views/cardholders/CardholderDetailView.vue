<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useFileUrl } from '@/composables/useFileUrl'
import { policyKey } from '@/utils/policyKey'
import { visitorPassState } from '@/utils/visitorPass'
import type { Cardholder, Credential, Role, AccessGroup, Portal } from '@/types/pocketbase'
import type { BadgeInviteResponse } from '@/types/badge'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import Avatar from '@/components/ui/Avatar.vue'
import BadgePreviewModal from '@/components/common/BadgePreviewModal.vue'
import VisitorReissueModal from '@/views/visitors/VisitorReissueModal.vue'
import { useAuthStore } from '@/stores/auth'
import type { SoftTone } from '@/utils/badges'

/**
 * One person, in full — staff, contractor, or visitor.
 *
 * # Why there is one page and not two
 *
 * A visitor IS a cardholder (`kind = "visitor"`), and this page and the old
 * `VisitorDetailView` had grown into near-copies: the same avatar header, the same
 * `password_set` → sign-in-methods derivation, the same "badge login with no credential"
 * alert, the same credentials list, the same badge preview. Two files describing one row is
 * how the same person ends up described two ways on two screens.
 *
 * So the visit is a SECTION here, not a page: when `kind = "visitor"` this page grows the
 * pass state, its window, and the four things you do to a visit — see their badge, mail them
 * the link again, reissue, revoke. Everything else is what it always was.
 *
 * # The one deliberate absence
 *
 * A visitor's pass is not edited from the credential form, it is REISSUED: a new credential
 * from the server's CSPRNG, with the previous visit's code revoked. Editing `valid_until` on
 * a live credential would silently extend a code that has already been photographed,
 * screenshotted, and possibly forwarded — which is why "+ Add credential" is hidden for a
 * visitor and Reissue takes its place.
 */
const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()
// cardholders.photo is a protected file — its URL needs a session file token.
const { url: fileUrl } = useFileUrl()

// No capability gate on reading the badge login: it is a field on this very record, so
// any operator who can see the cardholder can see it. That is deliberate — a blank space
// cannot say "not allowed to look".
const recordId = route.params.id as string
const record = ref<Cardholder | null>(null)
const credentials = ref<Credential[]>([])
const loading = ref(true)
const deleting = ref(false)
const working = ref(false)
const showReissue = ref(false)

const roles = computed<Role[]>(() => record.value?.expand?.roles || [])

const title = computed(() => record.value?.name || record.value?.email || 'Cardholder')
const kvKey = computed(() => (record.value ? policyKey('cardholders', record.value) : ''))

/**
 * The visitor fork. Blank `kind` means an ordinary cardholder (migration 1750000000), so
 * this asks the only question the schema guarantees an answer to: is this a visitor?
 */
const isVisitor = computed(() => record.value?.kind === 'visitor')
const canManage = computed(() => auth.can('enroll'))

/**
 * The pass, as one state — shared with the list page so the two cannot describe the same
 * visit differently. Derived from the CREDENTIAL rather than the person, because the
 * credential is what the edge enforces.
 */
const pass = computed(() => visitorPassState(record.value, credentials.value))
/** The credential whose window the state describes, for the dates below. */
const currentPass = computed<Credential | null>(() => pass.value.credential)

// Effective access: every portal reachable via this holder's
// roles → access groups → portals, deduped, with the granting groups.
// `id` lets RelationList key the rows.
interface EffectivePortal { id: string; portal: Portal; groups: string[] }
const effectiveAccess = computed<EffectivePortal[]>(() => {
  const byId = new Map<string, EffectivePortal>()
  for (const role of roles.value) {
    for (const group of (role.expand?.access_groups || []) as AccessGroup[]) {
      for (const portal of (group.expand?.portals || []) as Portal[]) {
        const existing = byId.get(portal.id)
        if (existing) {
          if (!existing.groups.includes(group.code)) existing.groups.push(group.code)
        } else {
          byId.set(portal.id, { id: portal.id, portal, groups: [group.code] })
        }
      }
    }
  }
  return [...byId.values()].sort((a, b) => a.portal.code.localeCompare(b.portal.code))
})

const credentialSearch = (c: Credential) => [c.value, c.label, c.type].filter(Boolean).join(' ')
const effectiveSearch = (ea: EffectivePortal) => [ea.portal.code, ea.portal.name, ...ea.groups].filter(Boolean).join(' ')

// --- badge login ---
//
// A field on this record, not a related entity: `cardholders` is itself the badge tier's
// auth collection, so "has a badge login" is `badge_login` on the row already loaded
// above. Shown here read-only and edited on the cardholder form like every other field.
//
// It is not access: their credentials work whether or not they can sign in. See
// docs/operators.md.
const hasBadgeLogin = computed(() => !!record.value?.badge_login)

/**
 * The sign-in methods actually available, in the order they are offered. `password_set`
 * is what distinguishes a password the holder knows from the random one every record
 * carries because PocketBase requires a non-blank password on an auth record.
 */
const badgeMethods = computed<string[]>(() => {
  if (!hasBadgeLogin.value) return []
  return record.value?.password_set ? ['Password', 'Emailed code'] : ['Emailed code']
})

/**
 * A badge login with no credential behind it is the state that produces a confusing
 * badge — the holder signs in and is told, correctly but unhelpfully, that no pass is
 * issued. Worth saying HERE, where the fix is one click away, rather than leaving the
 * operator to notice that "Credentials 0" and "Effective access 3" contradict a working
 * badge.
 */
const badgeHasNoCredential = computed(() => hasBadgeLogin.value && credentials.value.length === 0)

/**
 * A read-only look at what this person's own badge shows them (GET /api/badge/preview/{id}).
 *
 * Offered for every cardholder, not just visitors: "my badge doesn't work" is the same
 * support call whoever makes it, and the causes — no credential in window, a suspended
 * person, a group that grants nothing, a door that grants in person but not remotely — are
 * the same ones that take four collections to rule out by hand.
 *
 * Gated on `enroll` to match the route, and offered even without a badge login, because
 * "they cannot sign in at all" is one of the answers it gives.
 */
const showBadgePreview = ref(false)

async function load() {
  loading.value = true
  try {
    const [c, creds] = await Promise.all([
      pb.collection('cardholders').getOne<Cardholder>(recordId, {
        expand: 'roles,roles.access_groups,roles.access_groups.portals,operator',
      }),
      // Fetched newest-first because a visitor's pass history reads that way — the current
      // pass leads and the revoked ones fall below it. Re-sorted by value below for an
      // ordinary cardholder, whose cards have no meaningful order but a stable one.
      // One parallel round trip either way; the sort is the cheap part.
      pb.collection('credentials').getFullList<Credential>({ filter: `user = "${recordId}"`, sort: '-created' }),
    ])
    record.value = c
    credentials.value =
      c.kind === 'visitor'
        ? creds
        : [...creds].sort((a, b) => (a.value || '').localeCompare(b.value || ''))
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load cardholder')
    router.push('/cardholders')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const visitor = isVisitor.value
  const confirmed = await confirm({
    title: visitor ? 'Delete this visitor?' : 'Delete Cardholder',
    message: visitor ? `Delete ${title.value}?` : `Delete cardholder "${title.value}"?`,
    // Their credentials go with them (migration 1750000036 makes credentials.user
    // cascade), which is what an operator means by "delete this person" — a credential
    // outliving its holder is a key that opens doors and resolves to nobody. Their sign-in
    // goes too, because it is the same record. Say the card count, since that is the part
    // that surprises. For a visitor the surprise is the opposite one: what is destroyed is
    // the record that they were ever here, so revoke is named as the alternative.
    details: visitor
      ? 'Their pass and their sign-in are deleted with them, and the record that they visited is gone. Revoke instead if they may return.'
      : credentials.value.length
        ? `Their ${credentials.value.length} credential${credentials.value.length === 1 ? '' : 's'} will be deleted with them and will stop opening doors. This cannot be undone.`
        : 'This cannot be undone.',
    confirmText: visitor ? 'Delete visitor' : 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('cardholders').delete(recordId)
    toast.success(visitor ? 'Visitor deleted' : 'Cardholder deleted')
    router.push('/cardholders')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete cardholder')
  } finally {
    deleting.value = false
  }
}

/**
 * End the visit: revokes the credentials, keeps the person. The primary destructive action
 * for a visitor, because an operator reaching for a button here almost always means "stop
 * them getting in", not "erase the visit".
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

function dateLabel(raw?: string): string {
  if (!raw) return '—'
  const d = new Date(raw)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

function credTone(status: string): SoftTone {
  if (status === 'active') return 'success'
  if (status === 'revoked') return 'error'
  return 'warning'
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <DetailLayout
    v-else-if="record"
    :title="title"
    :breadcrumbs="[{ label: 'Cardholders', to: '/cardholders' }, { label: title }]"
  >
    <template #actions>
      <button v-if="canManage" class="btn btn-sm btn-ghost" @click="showBadgePreview = true">
        View their badge
      </button>
      <!-- Visit actions. Only reachable for a visitor: reissue mints from the visitor
           endpoint, and revoke ends a visit — neither means anything for a staff card. -->
      <template v-if="isVisitor && canManage">
        <button v-if="record.badge_login" class="btn btn-sm btn-ghost" :disabled="working" @click="resendInvite">
          Resend invite
        </button>
        <button class="btn btn-sm btn-ghost" :disabled="working" @click="showReissue = true">Reissue</button>
        <button
          v-if="pass.label === 'active'"
          class="btn btn-sm btn-ghost text-warning"
          :disabled="working"
          @click="revoke"
        >
          Revoke
        </button>
      </template>
      <router-link :to="`/cardholders/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting || working" @click="handleDelete">
        Delete
      </button>
    </template>

    <!-- Summary -->
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
        <!-- The pass state leads for a visitor, because "is this working right now" is the
             question that brought the operator here. -->
        <SoftBadge v-if="isVisitor" class="ml-auto shrink-0" :tone="pass.tone" dot>{{ pass.label }}</SoftBadge>
      </div>
      <div class="field-grid">
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Email">{{ record.email || '—' }}</DataField>
        <DataField label="Type">
          <SoftBadge :tone="isVisitor ? 'info' : 'neutral'">{{ isVisitor ? 'Visitor' : 'Permanent' }}</SoftBadge>
        </DataField>
        <!-- The visit's window, from the credential the state describes. -->
        <template v-if="isVisitor">
          <DataField label="Valid from">{{ dateLabel(currentPass?.valid_from) }}</DataField>
          <DataField label="Valid until">{{ dateLabel(currentPass?.valid_until) }}</DataField>
          <DataField label="Note">
            <span v-if="currentPass?.label">{{ currentPass.label }}</span>
            <span v-else class="opacity-40">—</span>
          </DataField>
        </template>
        <DataField label="External ID">
          <code v-if="record.external_id" class="text-xs">{{ record.external_id }}</code>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Status">
          <SoftBadge :tone="record.status === 'suspended' ? 'warning' : 'success'" dot>
            {{ record.status || 'active' }}
          </SoftBadge>
        </DataField>
        <!-- A field on this record, not a related entity. Edited on the form. -->
        <DataField label="Badge login">
          <span v-if="hasBadgeLogin" class="flex flex-wrap items-center gap-1">
            <span class="truncate">{{ record.email }}</span>
            <SoftBadge v-for="m in badgeMethods" :key="m" :tone="m === 'Password' ? 'success' : 'neutral'">
              {{ m }}
            </SoftBadge>
          </span>
          <span v-else class="opacity-40">None</span>
        </DataField>
        <!-- The same human's console account, when there is one. A pointer only: it lets
             them view this badge from their profile menu and grants nothing. -->
        <DataField label="Operator account">
          <router-link
            v-if="record.expand?.operator"
            :to="`/operators/${record.expand.operator.id}`"
            class="link link-primary truncate"
          >
            {{ record.expand.operator.email }}
          </router-link>
          <span v-else class="opacity-40">Not an operator</span>
        </DataField>
      </div>

      <!-- The state that makes a badge look broken to its holder. -->
      <div v-if="badgeHasNoCredential" class="alert alert-warning py-2 text-sm mt-4">
        <span v-if="isVisitor">
          This visitor has a sign-in but no pass, so their badge will say "no pass has been
          issued". Reissue to give them one.
        </span>
        <span v-else>
          This person has a badge login but no credential, so their badge will show
          "no pass has been issued". Add a credential below.
        </span>
      </div>
      <!-- Only worth saying for a visitor: a staff card works on a lanyard whether or not
           its holder ever signs in, but a visitor's pass lives on their phone. -->
      <div v-else-if="isVisitor && !hasBadgeLogin" class="alert alert-warning py-2 text-sm mt-4">
        <span>
          This visitor has no badge login, so they cannot sign in to see their pass at all.
          Turn on <strong>Badge login</strong> on their record.
        </span>
      </div>
    </BaseCard>

    <!-- Credentials (a credential belongs to this cardholder). For a visitor this is the
         visit history — the passes they have held, this visit and previous ones. That
         history IS the reason revoke keeps the record. -->
    <RelationList
      :title="isVisitor ? 'Passes' : 'Credentials'"
      icon="🎫"
      :items="credentials"
      :to="(c) => `/credentials/${c.id}`"
      :search-text="credentialSearch"
      :hint="isVisitor ? 'Newest first. A reissue revokes the previous code rather than extending it.' : undefined"
      :empty="isVisitor ? 'No pass has been issued for this visit.' : 'No credentials yet. Add a badge, PIN, or mobile credential for this person.'"
    >
      <!-- No manual add for a visitor: their pass value comes from the server's CSPRNG via
           Reissue, never from a form. -->
      <template v-if="!isVisitor" #actions>
        <router-link :to="`/credentials/new?user=${record.id}`" class="btn btn-sm btn-outline">+ Add credential</router-link>
      </template>
      <template #item="{ item: cred }">
        <code class="text-sm font-medium text-primary truncate">{{ cred.value }}</code>
        <SoftBadge v-if="!isVisitor">{{ cred.type || '—' }}</SoftBadge>
        <span v-if="cred.label" class="text-sm opacity-60 truncate flex-1">{{ cred.label }}</span>
        <span v-if="isVisitor" class="text-xs opacity-50 shrink-0">{{ dateLabel(cred.valid_until) }}</span>
        <SoftBadge class="ml-auto" :tone="credTone(cred.status || '')" dot>{{ cred.status || 'active' }}</SoftBadge>
      </template>
    </RelationList>

    <!-- Effective access -->
    <RelationList
      title="Effective access"
      icon="🎯"
      :items="effectiveAccess"
      :to="(ea) => `/portals/${ea.portal.id}`"
      :search-text="effectiveSearch"
      hint="Portals this person can reach through their roles — during each granting group's schedule."
      empty="No access yet. Assign roles whose access groups include some portals."
    >
      <template #item="{ item: ea }">
        <code class="text-sm font-medium text-primary">{{ ea.portal.code }}</code>
        <span class="text-sm opacity-60 truncate flex-1">{{ ea.portal.name }}</span>
        <span class="text-[10px] uppercase opacity-40 tracking-wide">via</span>
        <SoftBadge v-for="g in ea.groups" :key="g">{{ g }}</SoftBadge>
      </template>
    </RelationList>

    <!-- Roles. For a visitor this is the preset chosen at mint, replaced on each reissue. -->
    <RelationList
      :title="isVisitor ? 'Access preset' : 'Roles'"
      icon="🛡️"
      :items="roles"
      :to="(r) => `/roles/${r.id}`"
      :primary="(r) => r.code"
      :secondary="(r) => r.name"
      :hint="isVisitor ? 'A visit grants exactly the preset chosen when the pass was issued.' : undefined"
      :empty="isVisitor ? 'No preset assigned, so this pass opens nothing.' : 'No roles assigned.'"
    />

    <RecordMeta :record="record" :kv-key="kvKey" />

    <BadgePreviewModal v-model:open="showBadgePreview" :cardholder-id="record.id" :name="title" />
    <VisitorReissueModal
      v-if="isVisitor"
      v-model:open="showReissue"
      :cardholder="record"
      :current-role-id="roles[0]?.id || ''"
      @reissued="load"
    />
  </DetailLayout>
</template>
