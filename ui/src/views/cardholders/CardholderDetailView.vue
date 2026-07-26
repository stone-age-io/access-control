<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useFileUrl } from '@/composables/useFileUrl'
import { policyKey } from '@/utils/policyKey'
import type { Cardholder, Credential, Role, AccessGroup, Portal } from '@/types/pocketbase'
import type { BadgeUser } from '@/types/badge'
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
// cardholders.photo is a protected file — its URL needs a session file token.
const { url: fileUrl } = useFileUrl()

// No capability gate on reading the login: since migration 1750000035 any operator may
// read badge_users, so the field renders for everyone rather than vanishing for
// operators without `enroll` — a blank space cannot say "not allowed to look".
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
// A badge login is 1:1 with a cardholder, so it is shown here as a PROPERTY of the
// person — one read-only field — and edited on the cardholder form like every other
// field on this page. It used to be a card of its own with an inline issue/reset form,
// which made a one-line boolean look like a related entity and put a write surface on a
// read-only page.
//
// It is not access: their credentials work whether or not one exists. See
// docs/operators.md.
const badge = ref<BadgeUser | null>(null)

async function loadBadge() {
  try {
    badge.value = await pb
      .collection('badge_users')
      .getFirstListItem<BadgeUser>(`cardholder = "${recordId}"`)
  } catch {
    badge.value = null // absence is the normal case, not an error
  }
}

/**
 * The sign-in methods actually available on this login, in the order they are offered.
 * `password_set` is what distinguishes a real password from the throwaway PocketBase
 * requires on every auth record.
 */
const badgeMethods = computed<string[]>(() => {
  if (!badge.value) return []
  return badge.value.password_set ? ['Password', 'Emailed code'] : ['Emailed code']
})

/**
 * A badge login with no credential behind it is the state that produces a confusing
 * badge — the holder signs in and is told, correctly but unhelpfully, that no pass is
 * issued. Worth saying HERE, where the fix is one click away, rather than leaving the
 * operator to notice that "Credentials 0" and "Effective access 3" contradict a working
 * badge.
 */
const badgeHasNoCredential = computed(() => !!badge.value && credentials.value.length === 0)

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
    // Accurate on both counts: a credential is a REQUIRED reference, so PocketBase
    // refuses the delete outright while any exist, and the badge login cascades away
    // with the person (migration 1750000035).
    details: credentials.value.length
      ? 'Delete or reassign their credentials first — a credential cannot be left without a holder.'
      : 'Their badge login is removed with them. This cannot be undone.',
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
        <!-- 1:1 with this cardholder, so it reads as a field. Edited on the form. -->
        <DataField label="Badge login">
          <span v-if="badge" class="flex flex-wrap items-center gap-1">
            <span class="truncate">{{ badge.email }}</span>
            <SoftBadge v-for="m in badgeMethods" :key="m" :tone="m === 'Password' ? 'success' : 'neutral'">
              {{ m }}
            </SoftBadge>
          </span>
          <span v-else class="opacity-40">None</span>
        </DataField>
      </div>

      <!-- The state that makes a badge look broken to its holder. -->
      <div v-if="badgeHasNoCredential" class="alert alert-warning py-2 text-sm mt-4">
        <span>
          This person has a badge login but no credential, so their badge will show
          "no pass has been issued". Add a credential below.
        </span>
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
