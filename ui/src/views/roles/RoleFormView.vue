<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Role, AccessGroup } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  code: '',
  name: '',
  access_groups: [] as string[],
})

const groups = ref<AccessGroup[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('roles', { code: form.value.code.trim() }))

async function loadOptions() {
  try {
    groups.value = await pb.collection('access_groups').getFullList<AccessGroup>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const r = await pb.collection('roles').getOne<Role>(recordId)
    form.value = {
      code: r.code || '',
      name: r.name || '',
      access_groups: [...(r.access_groups || [])],
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load role')
    router.push('/roles')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      access_groups: form.value.access_groups,
    }
    if (isEdit.value) {
      await pb.collection('roles').update(recordId!, data)
      toast.success('Role updated')
      router.push(`/roles/${recordId}`)
    } else {
      const created = await pb.collection('roles').create<Role>(data)
      toast.success('Role created')
      router.push(`/roles/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save role')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadOptions()
  if (isEdit.value) await loadRecord()
})
</script>

<template>
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <DetailLayout
      :title="isEdit ? 'Edit Role' : 'New Role'"
      :breadcrumbs="[{ label: 'Roles', to: '/roles' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Role">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <FormField label="Code" required>
            <input v-model="form.code" type="text" placeholder="staff" class="input input-bordered font-mono" required />
          </FormField>
          <FormField label="Name">
            <input v-model="form.name" type="text" placeholder="Staff" class="input input-bordered" />
          </FormField>
        </div>
      </BaseCard>

      <BaseCard title="Access Groups">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">The access groups this role grants to cardholders.</p>
          <div class="border border-base-300 rounded-box p-3 max-h-64 overflow-y-auto space-y-1">
            <label v-for="g in groups" :key="g.id" class="flex items-center gap-3 cursor-pointer py-1 px-1 rounded hover:bg-base-200">
              <input type="checkbox" class="checkbox checkbox-sm" :value="g.id" v-model="form.access_groups" />
              <code class="text-sm font-medium">{{ g.code }}</code>
              <span class="text-sm opacity-50 truncate">{{ g.name }}</span>
            </label>
            <p v-if="groups.length === 0" class="text-sm opacity-50 py-2">No access groups available. Create some first.</p>
          </div>
          <p class="text-xs opacity-50">{{ form.access_groups.length }} selected</p>
        </div>
      </BaseCard>

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">role.&lt;code&gt;</code>
          <p class="text-xs opacity-50">The mirror writes this role to the policy bucket under this key.</p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Role</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
