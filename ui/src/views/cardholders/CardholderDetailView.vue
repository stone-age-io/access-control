<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Cardholder, Credential, Role, AccessGroup, AccessPoint } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RefList from '@/components/ui/RefList.vue'

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

// Effective access: every access point reachable via this holder's
// roles → access groups → access points, deduped, with the granting groups.
interface EffectivePoint { point: AccessPoint; groups: string[] }
const effectiveAccess = computed<EffectivePoint[]>(() => {
  const byId = new Map<string, EffectivePoint>()
  for (const role of roles.value) {
    for (const group of (role.expand?.access_groups || []) as AccessGroup[]) {
      for (const point of (group.expand?.access_points || []) as AccessPoint[]) {
        const existing = byId.get(point.id)
        if (existing) {
          if (!existing.groups.includes(group.code)) existing.groups.push(group.code)
        } else {
          byId.set(point.id, { point, groups: [group.code] })
        }
      }
    }
  }
  return [...byId.values()].sort((a, b) => a.point.code.localeCompare(b.point.code))
})

async function load() {
  loading.value = true
  try {
    const [c, creds] = await Promise.all([
      pb.collection('cardholders').getOne<Cardholder>(recordId, {
        expand: 'roles,roles.access_groups,roles.access_groups.access_points',
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
    <BaseCard title="Cardholder">
      <div class="grid grid-cols-2 gap-x-6 gap-y-4">
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
    <BaseCard title="Credentials">
      <template #actions>
        <router-link :to="`/credentials/new?user=${record.id}`" class="btn btn-sm btn-outline">+ Add credential</router-link>
      </template>
      <div v-if="credentials.length === 0" class="text-center py-6 text-sm opacity-50">
        No credentials yet. Add a badge, PIN, or mobile credential for this person.
      </div>
      <ul v-else class="divide-y divide-base-200">
        <li
          v-for="cred in credentials"
          :key="cred.id"
          class="flex items-center gap-3 py-2.5 px-1 -mx-1 rounded hover:bg-base-200 cursor-pointer transition-colors"
          @click="router.push(`/credentials/${cred.id}`)"
        >
          <code class="text-sm font-medium text-primary truncate">{{ cred.value }}</code>
          <span class="badge badge-ghost badge-sm">{{ cred.type || '—' }}</span>
          <span v-if="cred.label" class="text-sm opacity-60 truncate flex-1">{{ cred.label }}</span>
          <span class="badge badge-sm ml-auto" :class="credBadge(cred.status || '')">{{ cred.status || 'active' }}</span>
        </li>
      </ul>
    </BaseCard>

    <!-- Effective access -->
    <BaseCard title="Effective Access">
      <template #actions>
        <span class="text-xs opacity-50">{{ effectiveAccess.length }} point(s)</span>
      </template>
      <p class="text-sm text-base-content/60 mb-3">
        Access points this person can reach through their roles — during each granting group's schedule.
      </p>
      <div v-if="effectiveAccess.length === 0" class="text-center py-6 text-sm opacity-50">
        No access yet. Assign roles whose access groups include some access points.
      </div>
      <ul v-else class="divide-y divide-base-200">
        <li
          v-for="ea in effectiveAccess"
          :key="ea.point.id"
          class="flex items-center gap-3 py-2.5 px-1 -mx-1 rounded hover:bg-base-200 cursor-pointer transition-colors"
          @click="router.push(`/access-points/${ea.point.id}`)"
        >
          <code class="text-sm font-medium text-primary">{{ ea.point.code }}</code>
          <span class="text-sm opacity-60 truncate flex-1">{{ ea.point.name }}</span>
          <span class="text-[10px] uppercase opacity-40 tracking-wide">via</span>
          <span v-for="g in ea.groups" :key="g" class="badge badge-ghost badge-sm">{{ g }}</span>
        </li>
      </ul>
    </BaseCard>

    <!-- Rail -->
    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="Roles"
        icon="🛡️"
        :items="roles"
        :to="(r) => `/roles/${r.id}`"
        :primary="(r) => r.code"
        :secondary="(r) => r.name"
        empty="No roles assigned."
      />
    </template>
  </DetailLayout>
</template>
