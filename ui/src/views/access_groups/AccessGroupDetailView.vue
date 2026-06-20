<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { AccessGroup, Role } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RailCard from '@/components/ui/RailCard.vue'
import RefList from '@/components/ui/RefList.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<AccessGroup | null>(null)
const roles = ref<Role[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Access Group')
const kvKey = computed(() => (record.value ? policyKey('access_groups', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [g, r] = await Promise.all([
      pb.collection('access_groups').getOne<AccessGroup>(recordId, { expand: 'portals,schedule' }),
      pb.collection('roles').getFullList<Role>({ filter: `access_groups ~ "${recordId}"`, sort: 'code' }),
    ])
    record.value = g
    roles.value = r
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load access group')
    router.push('/access-groups')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Access Group',
    message: `Delete access group "${record.value.code}"?`,
    details: 'Roles referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('access_groups').delete(recordId)
    toast.success('Access group deleted')
    router.push('/access-groups')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete access group')
  } finally {
    deleting.value = false
  }
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
    :breadcrumbs="[{ label: 'Access Groups', to: '/access-groups' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/access-groups/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <!-- Summary -->
    <BaseCard title="Access Group">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Schedule">
          <router-link
            v-if="record.expand?.schedule"
            :to="`/schedules/${record.expand.schedule.id}`"
            class="link link-primary"
          >
            {{ record.expand.schedule.code }}
          </router-link>
          <span v-else class="opacity-40">—</span>
        </DataField>
      </div>
    </BaseCard>

    <!-- Portals in this group -->
    <BaseCard title="Portals">
      <div v-if="(record.expand?.portals || []).length === 0" class="text-center py-6 text-sm opacity-50">
        No portals in this group.
      </div>
      <ul v-else class="divide-y divide-base-200">
        <li
          v-for="p in record.expand?.portals || []"
          :key="p.id"
          class="flex items-center gap-3 py-2.5 px-1 -mx-1 rounded hover:bg-base-200 cursor-pointer transition-colors"
          @click="router.push(`/portals/${p.id}`)"
        >
          <code class="text-sm font-medium text-primary">{{ p.code }}</code>
          <span class="text-sm opacity-60 truncate flex-1">{{ p.name }}</span>
          <span class="badge badge-ghost badge-sm">{{ p.posture || '—' }}</span>
        </li>
      </ul>
    </BaseCard>

    <!-- Rail -->
    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RailCard v-if="record.expand?.schedule" title="Schedule" icon="🗓️">
        <router-link
          :to="`/schedules/${record.expand.schedule.id}`"
          class="flex flex-col gap-0.5 hover:opacity-80"
        >
          <code class="text-sm font-medium text-primary">{{ record.expand.schedule.code }}</code>
          <span v-if="record.expand.schedule.name" class="text-xs opacity-50">{{ record.expand.schedule.name }}</span>
        </router-link>
      </RailCard>
      <RefList
        title="Used by roles"
        icon="🛡️"
        :items="roles"
        :to="(r) => `/roles/${r.id}`"
        :primary="(r) => r.code"
        :secondary="(r) => r.name"
        empty="Not used by any role yet."
      />
    </template>
  </DetailLayout>
</template>
