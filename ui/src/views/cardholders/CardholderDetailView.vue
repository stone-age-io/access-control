<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useFileUrl } from '@/composables/useFileUrl'
import { useAuthStore } from '@/stores/auth'
import { policyKey } from '@/utils/policyKey'
import type { Cardholder, Credential, Role, AccessGroup, Portal } from '@/types/pocketbase'
import type { BadgeUser, IssueHolderResponse } from '@/types/badge'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import Avatar from '@/components/ui/Avatar.vue'
import type { SoftTone } from '@/utils/badges'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()
// cardholders.photo is a protected file — its URL needs a session file token.
const { url: fileUrl } = useFileUrl()

// Only `enroll` may list or create badge logins (migration 1750000030), so the whole
// card is hidden for anyone else rather than showing a permanently failing panel.
const canEnroll = computed(() => auth.can('enroll'))
// Deleting a badge login needs `operators`, not `enroll` — deliberately asymmetric,
// since removing someone's login is closer to account administration than enrollment.
const canRemoveBadge = computed(() => auth.can('operators'))

const recordId = route.params.id as string
const record = ref<Cardholder | null>(null)
const credentials = ref<Credential[]>([])
const loading = ref(true)
const deleting = ref(false)

const roles = computed<Role[]>(() => record.value?.expand?.roles || [])

const title = computed(() => record.value?.name || record.value?.email || 'Cardholder')
const kvKey = computed(() => (record.value ? policyKey('cardholders', record.value) : ''))

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

// --- badge login (badge_users) ---
//
// A badge login is what lets this person SEE their badge; it is not access. Their card
// keeps working whether or not one exists, and removing one revokes nothing — see
// docs/operators.md. Issuing goes through POST /api/badge/holders rather than the
// collection API so the `kind`, throwaway-password and `password_set` semantics live in
// one place server-side.
const badge = ref<BadgeUser | null>(null)
const badgeLoading = ref(false)
const showIssueForm = ref(false)
const issuing = ref(false)
const issueEmail = ref('')
const issuePassword = ref('')
const issueSendInvite = ref(true)
const issueError = ref('')

async function loadBadge() {
  if (!canEnroll.value) return
  badgeLoading.value = true
  try {
    badge.value = await pb
      .collection('badge_users')
      .getFirstListItem<BadgeUser>(`cardholder = "${recordId}"`)
  } catch {
    badge.value = null // absence is the normal case, not an error
  } finally {
    badgeLoading.value = false
  }
}

function openIssueForm() {
  issueError.value = ''
  issuePassword.value = ''
  issueEmail.value = badge.value?.email || record.value?.email || ''
  issueSendInvite.value = !badge.value
  showIssueForm.value = true
}

async function submitIssue() {
  issueError.value = ''
  if (issuePassword.value && issuePassword.value.length < 8) {
    issueError.value = 'A password must be at least 8 characters.'
    return
  }
  issuing.value = true
  try {
    const res = await pb.send<IssueHolderResponse>('/api/badge/holders', {
      method: 'POST',
      body: {
        cardholder: recordId,
        email: issueEmail.value.trim(),
        password: issuePassword.value,
        sendInvite: issueSendInvite.value,
      },
    })
    showIssueForm.value = false
    issuePassword.value = ''
    await loadBadge()

    toast.success(res.created ? 'Badge login issued' : 'Badge login updated')
    if (issueSendInvite.value && !res.inviteSent) {
      // Not a failure: the login works regardless. Say so, because the operator now
      // has to hand over the sign-in details themselves.
      toast.error('The invite email could not be sent — no SMTP configured? Give them the details in person.')
    }
  } catch (err: any) {
    issueError.value = err?.response?.message || err?.message || 'Failed to issue the badge login'
  } finally {
    issuing.value = false
  }
}

async function removeBadge() {
  if (!badge.value) return
  const confirmed = await confirm({
    title: 'Remove badge login',
    message: `Remove the badge login for "${badge.value.email}"?`,
    details:
      'This only removes their ability to SEE their badge and use remote unlock. Their credentials keep working — to revoke access, revoke the credential instead.',
    confirmText: 'Remove login',
    variant: 'warning',
  })
  if (!confirmed) return
  try {
    await pb.collection('badge_users').delete(badge.value.id)
    badge.value = null
    toast.success('Badge login removed')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to remove the badge login')
  }
}

async function load() {
  loading.value = true
  try {
    const [c, creds] = await Promise.all([
      pb.collection('cardholders').getOne<Cardholder>(recordId, {
        expand: 'roles,roles.access_groups,roles.access_groups.portals',
      }),
      pb.collection('credentials').getFullList<Credential>({ filter: `user = "${recordId}"`, sort: 'value' }),
    ])
    record.value = c
    credentials.value = creds
    await loadBadge()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load cardholder')
    router.push('/cardholders')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Cardholder',
    message: `Delete cardholder "${title.value}"?`,
    details: 'Their credentials will be left without a holder. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('cardholders').delete(recordId)
    toast.success('Cardholder deleted')
    router.push('/cardholders')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete cardholder')
  } finally {
    deleting.value = false
  }
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
      <router-link :to="`/cardholders/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
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
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Email">{{ record.email || '—' }}</DataField>
        <DataField label="External ID">
          <code v-if="record.external_id" class="text-xs">{{ record.external_id }}</code>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Status">
          <SoftBadge :tone="record.status === 'suspended' ? 'warning' : 'success'" dot>
            {{ record.status || 'active' }}
          </SoftBadge>
        </DataField>
      </div>
    </BaseCard>

    <!-- Credentials (a credential belongs to this cardholder) -->
    <RelationList
      title="Credentials"
      icon="🎫"
      :items="credentials"
      :to="(c) => `/credentials/${c.id}`"
      :search-text="credentialSearch"
      empty="No credentials yet. Add a badge, PIN, or mobile credential for this person."
    >
      <template #actions>
        <router-link :to="`/credentials/new?user=${record.id}`" class="btn btn-sm btn-outline">+ Add credential</router-link>
      </template>
      <template #item="{ item: cred }">
        <code class="text-sm font-medium text-primary truncate">{{ cred.value }}</code>
        <SoftBadge>{{ cred.type || '—' }}</SoftBadge>
        <span v-if="cred.label" class="text-sm opacity-60 truncate flex-1">{{ cred.label }}</span>
        <SoftBadge class="ml-auto" :tone="credTone(cred.status || '')" dot>{{ cred.status || 'active' }}</SoftBadge>
      </template>
    </RelationList>

    <!-- Badge login: whether this person can SEE their badge. Not access — their
         credentials work regardless. Enroll-gated, because only `enroll` may read or
         create badge_users. -->
    <BaseCard v-if="canEnroll">
      <div class="flex items-start justify-between gap-3 mb-4">
        <div class="min-w-0">
          <h2 class="font-semibold flex items-center gap-2"><span>📱</span><span>Badge login</span></h2>
          <p class="text-xs text-base-content/60 mt-1">
            Lets this person view their badge on a phone and use remote unlock where a door allows it.
            It grants no access of its own — their credentials decide that.
          </p>
        </div>
        <div class="flex gap-2 shrink-0">
          <button v-if="!showIssueForm" class="btn btn-sm btn-outline" @click="openIssueForm">
            {{ badge ? 'Reset password' : 'Issue login' }}
          </button>
          <button v-else class="btn btn-sm btn-ghost" @click="showIssueForm = false">Cancel</button>
          <button
            v-if="badge && canRemoveBadge && !showIssueForm"
            class="btn btn-sm btn-ghost text-error"
            @click="removeBadge"
          >
            Remove
          </button>
        </div>
      </div>

      <div v-if="badgeLoading" class="flex justify-center py-4">
        <span class="loading loading-spinner"></span>
      </div>

      <!-- Existing login -->
      <div v-else-if="badge && !showIssueForm" class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Sign-in email">{{ badge.email || '—' }}</DataField>
        <DataField label="Type">
          <SoftBadge :tone="badge.kind === 'visitor' ? 'warning' : 'info'">{{ badge.kind }}</SoftBadge>
        </DataField>
        <DataField label="Sign-in methods">
          <span class="flex flex-wrap gap-1">
            <SoftBadge v-if="badge.password_set" tone="success" dot>Password</SoftBadge>
            <SoftBadge tone="neutral">Emailed code</SoftBadge>
          </span>
        </DataField>
      </div>

      <p v-else-if="!badge && !showIssueForm" class="text-sm text-base-content/60">
        No badge login. This person's credentials still work at every door they are entitled to —
        they simply have no way to view their badge.
      </p>

      <!-- Issue / reset -->
      <form v-if="showIssueForm" class="space-y-4" @submit.prevent="submitIssue">
        <div v-if="issueError" class="alert alert-error py-2 text-sm">{{ issueError }}</div>

        <label class="form-control">
          <span class="label-text mb-1">Sign-in email</span>
          <input
            v-model="issueEmail"
            type="email"
            inputmode="email"
            placeholder="them@example.com"
            class="input input-bordered input-sm"
            :disabled="issuing"
          />
          <span class="label-text-alt mt-1 opacity-60">Where their sign-in codes go. Defaults to the cardholder's email.</span>
        </label>

        <label class="form-control">
          <span class="label-text mb-1">
            {{ badge?.password_set ? 'New password' : 'Initial password' }}
            <span class="opacity-50">(optional)</span>
          </span>
          <input
            v-model="issuePassword"
            type="text"
            autocomplete="off"
            placeholder="Leave blank for emailed-code sign-in only"
            class="input input-bordered input-sm font-mono"
            :disabled="issuing"
          />
          <span class="label-text-alt mt-1 opacity-60">
            Hand this over in person — it is never emailed. Set one if this install has no mail server,
            since an emailed code would otherwise be the only way in. They can change it from their badge.
          </span>
        </label>

        <label class="label cursor-pointer justify-start gap-3">
          <input v-model="issueSendInvite" type="checkbox" class="checkbox checkbox-sm" :disabled="issuing" />
          <span class="label-text">Email them where to sign in (no password included)</span>
        </label>

        <button type="submit" class="btn btn-primary btn-sm" :disabled="issuing">
          <span v-if="issuing" class="loading loading-spinner loading-sm"></span>
          <span v-else>{{ badge ? 'Update login' : 'Issue login' }}</span>
        </button>
      </form>
    </BaseCard>

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

    <!-- Roles -->
    <RelationList
      title="Roles"
      icon="🛡️"
      :items="roles"
      :to="(r) => `/roles/${r.id}`"
      :primary="(r) => r.code"
      :secondary="(r) => r.name"
      empty="No roles assigned."
    />

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>
