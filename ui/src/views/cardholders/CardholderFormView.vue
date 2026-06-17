<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Cardholder, CardholderStatus, Role } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const STATUSES: CardholderStatus[] = ['active', 'suspended']

const form = ref({
  external_id: '',
  name: '',
  email: '',
  status: 'active' as CardholderStatus,
  roles: [] as string[],
})

const roles = ref<Role[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

async function loadOptions() {
  try {
    roles.value = await pb.collection('roles').getFullList<Role>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load roles')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const c = await pb.collection('cardholders').getOne<Cardholder>(recordId)
    form.value = {
      external_id: c.external_id || '',
      name: c.name || '',
      email: c.email || '',
      status: (c.status || 'active') as CardholderStatus,
      roles: [...(c.roles || [])],
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load cardholder')
    router.push('/cardholders')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.name.trim() && !form.value.email.trim()) { toast.error('Name or email is required'); return }

  loading.value = true
  try {
    const data = {
      external_id: form.value.external_id.trim(),
      name: form.value.name.trim(),
      email: form.value.email.trim(),
      status: form.value.status,
      roles: form.value.roles,
    }
    if (isEdit.value) {
      await pb.collection('cardholders').update(recordId!, data)
      toast.success('Cardholder updated')
    } else {
      await pb.collection('cardholders').create(data)
      toast.success('Cardholder created')
    }
    router.push('/cardholders')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save cardholder')
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
  <div class="space-y-6 max-w-2xl">
    <div>
      <div class="breadcrumbs text-sm">
        <ul>
          <li><router-link to="/cardholders">Cardholders</router-link></li>
          <li>{{ isEdit ? 'Edit' : 'New' }}</li>
        </ul>
      </div>
      <h1 class="text-3xl font-bold">{{ isEdit ? 'Edit Cardholder' : 'New Cardholder' }}</h1>
    </div>

    <div v-if="loadingRecord" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-6">
      <BaseCard title="Cardholder">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Name</span></label>
              <input v-model="form.name" type="text" placeholder="Alice Smith" class="input input-bordered" />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Email</span></label>
              <input v-model="form.email" type="email" placeholder="alice@example.com" class="input input-bordered" />
            </div>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">External ID</span></label>
              <input v-model="form.external_id" type="text" placeholder="ldap-12345" class="input input-bordered font-mono" />
              <label class="label"><span class="label-text-alt">Optional IdP/LDAP/CSV key.</span></label>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Status</span></label>
              <select v-model="form.status" class="select select-bordered">
                <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
              </select>
            </div>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Roles">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">The roles assigned to this cardholder.</p>
          <div class="border border-base-300 rounded-box p-3 max-h-64 overflow-y-auto space-y-1">
            <label v-for="r in roles" :key="r.id" class="flex items-center gap-3 cursor-pointer py-1 px-1 rounded hover:bg-base-200">
              <input type="checkbox" class="checkbox checkbox-sm" :value="r.id" v-model="form.roles" />
              <code class="text-sm font-medium">{{ r.code }}</code>
              <span class="text-sm opacity-50 truncate">{{ r.name }}</span>
            </label>
            <p v-if="roles.length === 0" class="text-sm opacity-50 py-2">No roles available. Create some first.</p>
          </div>
          <p class="text-xs opacity-50">{{ form.roles.length }} selected</p>
        </div>
      </BaseCard>

      <div class="flex flex-col sm:flex-row justify-end gap-2 sm:gap-4">
        <button type="button" @click="router.back()" class="btn btn-ghost order-2 sm:order-1" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary order-1 sm:order-2" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Cardholder</span>
        </button>
      </div>
    </form>
  </div>
</template>
