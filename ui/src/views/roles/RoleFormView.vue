<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Role, AccessGroup } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import RelationPicker from '@/components/ui/RelationPicker.vue'

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
    <FormLayout
      :title="isEdit ? 'Edit Role' : 'New Role'"
      :breadcrumbs="[{ label: 'Roles', to: '/roles' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'role.<code>'"
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
          <RelationPicker
            v-model="form.access_groups"
            :options="groups"
            :primary="(g) => g.code"
            :secondary="(g) => g.name"
            empty="No access groups available. Create some first."
          />
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Role</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
