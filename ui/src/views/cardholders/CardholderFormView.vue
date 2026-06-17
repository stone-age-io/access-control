<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Cardholder, CardholderStatus, Role } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'

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

const kvKey = computed(() => (recordId ? `user.${recordId}` : ''))

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
      router.push(`/cardholders/${recordId}`)
    } else {
      const created = await pb.collection('cardholders').create<Cardholder>(data)
      toast.success('Cardholder created')
      router.push(`/cardholders/${created.id}`)
    }
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
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <DetailLayout
      :title="isEdit ? 'Edit Cardholder' : 'New Cardholder'"
      :breadcrumbs="[{ label: 'Cardholders', to: '/cardholders' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
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

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">user.&lt;assigned on save&gt;</code>
          <p class="text-xs opacity-50">Cardholders are keyed by their record id in the ACC_POLICY bucket.</p>
        </RailCard>
        <RailCard title="About cardholders" icon="🪪">
          <p class="text-xs opacity-60 leading-relaxed">
            A cardholder is a person who holds credentials — not a PocketBase login. Roles grant access groups;
            add their badges and PINs from the cardholder page once saved.
          </p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Cardholder</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
