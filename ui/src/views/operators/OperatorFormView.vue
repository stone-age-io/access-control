<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { User, Capability } from '@/types/pocketbase'
import { CAPABILITIES, PRESETS, presetLabel } from '@/utils/capabilities'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  email: '',
  name: '',
  permissions: [] as Capability[],
  verified: true,
  notify: false,
  password: '',
  passwordConfirm: '',
})

const loading = ref(false)
const loadingRecord = ref(false)

// The preset whose capability set matches the current selection (else "Custom").
const currentPreset = computed(() => presetLabel(form.value.permissions))
function applyPreset(caps: readonly Capability[]) {
  form.value.permissions = [...caps]
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const u = await pb.collection('users').getOne<User>(recordId)
    form.value = {
      email: u.email || '',
      name: u.name || '',
      permissions: (u.permissions || []) as Capability[],
      verified: !!u.verified,
      notify: !!u.notify,
      password: '',
      passwordConfirm: '',
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load operator')
    router.push('/operators')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.email.trim()) { toast.error('Email is required'); return }
  if (!isEdit.value && !form.value.password) { toast.error('Password is required for a new operator'); return }
  if (form.value.password && form.value.password !== form.value.passwordConfirm) {
    toast.error('Passwords do not match'); return
  }

  loading.value = true
  try {
    const data: Record<string, any> = {
      email: form.value.email.trim(),
      name: form.value.name.trim(),
      permissions: form.value.permissions,
      verified: form.value.verified,
      notify: form.value.notify,
    }
    // Password is set on create, and on edit only when a new one was entered.
    if (form.value.password) {
      data.password = form.value.password
      data.passwordConfirm = form.value.passwordConfirm
    }
    if (isEdit.value) {
      await pb.collection('users').update(recordId!, data)
      toast.success('Operator updated')
    } else {
      await pb.collection('users').create<User>(data)
      toast.success('Operator created')
    }
    router.push('/operators')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save operator')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (isEdit.value) loadRecord()
})
</script>

<template>
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <FormLayout
      :title="isEdit ? 'Edit Operator' : 'New Operator'"
      :breadcrumbs="[{ label: 'Operators', to: '/operators' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Operator">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Email" required>
              <input v-model="form.email" type="email" placeholder="jane@example.com" class="input input-bordered" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Jane Operator" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Verified" hint="Verified accounts can sign in.">
              <label class="label cursor-pointer justify-start gap-3">
                <input v-model="form.verified" type="checkbox" class="toggle toggle-primary" />
                <span class="label-text">{{ form.verified ? 'Verified' : 'Unverified' }}</span>
              </label>
            </FormField>
            <FormField label="Notify" hint="Email this operator on alarms from sources that opt into email (portals/areas/locations).">
              <label class="label cursor-pointer justify-start gap-3">
                <input v-model="form.notify" type="checkbox" class="toggle toggle-primary" />
                <span class="label-text">{{ form.notify ? 'Receives alarm email' : 'No alarm email' }}</span>
              </label>
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Password" :hint="isEdit ? 'Leave blank to keep the current password.' : 'Required.'">
              <input v-model="form.password" type="password" placeholder="••••••••" class="input input-bordered" :required="!isEdit" autocomplete="new-password" />
            </FormField>
            <FormField label="Confirm password">
              <input v-model="form.passwordConfirm" type="password" placeholder="••••••••" class="input input-bordered" autocomplete="new-password" />
            </FormField>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Permissions">
        <div class="space-y-4">
          <!-- Quick-apply presets. They just tick capabilities below; nothing
               about a preset is stored — permissions are the source of truth. -->
          <div>
            <div class="flex items-center justify-between mb-2">
              <span class="text-sm font-medium opacity-70">Presets</span>
              <span class="badge badge-ghost badge-sm">{{ currentPreset }}</span>
            </div>
            <div class="flex flex-wrap gap-2">
              <button
                v-for="p in PRESETS"
                :key="p.name"
                type="button"
                class="btn btn-sm"
                :class="p.name === currentPreset ? 'btn-primary' : 'btn-outline'"
                @click="applyPreset(p.caps)"
              >
                {{ p.name }}
              </button>
            </div>
          </div>

          <div class="divider my-0"></div>

          <!-- The capability checklist is the actual grant. Reads need none. -->
          <div class="space-y-2">
            <label
              v-for="c in CAPABILITIES"
              :key="c.value"
              class="flex items-start gap-3 p-2 rounded-lg hover:bg-base-200 cursor-pointer"
            >
              <input
                v-model="form.permissions"
                type="checkbox"
                :value="c.value"
                class="checkbox checkbox-primary mt-0.5"
              />
              <span class="flex flex-col">
                <span class="font-medium">{{ c.label }}</span>
                <span class="text-xs text-base-content/60">{{ c.hint }}</span>
              </span>
            </label>
            <p class="text-xs text-base-content/60 px-2">
              Any authenticated operator can view everything; these gate edits and commands.
            </p>
          </div>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Operator</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
