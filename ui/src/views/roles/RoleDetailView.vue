<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Role, AccessGroup, Cardholder } from '@/types/pocketbase'
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
const record = ref<Role | null>(null)
const cardholders = ref<Cardholder[]>([])
const loading = ref(true)
const deleting = ref(false)

const accessGroups = computed<AccessGroup[]>(() => record.value?.expand?.access_groups || [])

const title = computed(() => record.value?.name || record.value?.code || 'Role')
const kvKey = computed(() => (record.value ? policyKey('roles', record.value) : ''))

async function load() {
  loading.value = true
  try {
    const [r, holders] = await Promise.all([
      pb.collection('roles').getOne<Role>(recordId, { expand: 'access_groups' }),
      pb.collection('cardholders').getFullList<Cardholder>({ filter: `roles ~ "${recordId}"`, sort: 'name' }),
    ])
    record.value = r
    cardholders.value = holders
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load role')
    router.push('/roles')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Role',
    message: `Delete role "${title.value}"?`,
    details: 'Cardholders referencing it will drop the membership. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('roles').delete(recordId)
    toast.success('Role deleted')
    router.push('/roles')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete role')
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
    :breadcrumbs="[{ label: 'Roles', to: '/roles' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/roles/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <!-- Summary -->
    <BaseCard title="Role">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
      </div>
    </BaseCard>

    <!-- Access groups in this role -->
    <BaseCard title="Access Groups">
      <div v-if="accessGroups.length === 0" class="text-center py-6 text-sm opacity-50">
        No access groups in this role.
      </div>
      <ul v-else class="divide-y divide-base-200">
        <li
          v-for="g in accessGroups"
          :key="g.id"
          class="flex items-center gap-3 py-2.5 px-1 -mx-1 rounded hover:bg-base-200 cursor-pointer transition-colors"
          @click="router.push(`/access-groups/${g.id}`)"
        >
          <code class="text-sm font-medium text-primary">{{ g.code }}</code>
          <span class="text-sm opacity-60 truncate flex-1">{{ g.name }}</span>
        </li>
      </ul>
    </BaseCard>

    <!-- Rail -->
    <template #rail>
      <RecordMeta :record="record" :kv-key="kvKey" />
      <RefList
        title="Cardholders"
        icon="🪪"
        :items="cardholders"
        :to="(c) => `/cardholders/${c.id}`"
        :primary="(c) => c.name || c.email || c.id"
        :secondary="(c) => c.email"
        empty="No cardholders have this role."
      />
    </template>
  </DetailLayout>
</template>
