<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Cardholder, Credential, Role, AccessGroup, Portal } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

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

function credBadge(status: string): string {
  if (status === 'active') return 'badge-success'
  if (status === 'revoked') return 'badge-error'
  return 'badge-warning'
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
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Email">{{ record.email || '—' }}</DataField>
        <DataField label="External ID">
          <code v-if="record.external_id" class="text-xs">{{ record.external_id }}</code>
          <span v-else class="opacity-40">—</span>
        </DataField>
        <DataField label="Status">
          <span class="badge badge-sm" :class="record.status === 'suspended' ? 'badge-warning' : 'badge-success'">
            {{ record.status || 'active' }}
          </span>
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
        <span class="badge badge-ghost badge-sm">{{ cred.type || '—' }}</span>
        <span v-if="cred.label" class="text-sm opacity-60 truncate flex-1">{{ cred.label }}</span>
        <span class="badge badge-sm ml-auto" :class="credBadge(cred.status || '')">{{ cred.status || 'active' }}</span>
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
        <span v-for="g in ea.groups" :key="g" class="badge badge-ghost badge-sm">{{ g }}</span>
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
