<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { AccessGroup, Portal, Schedule } from '@/types/pocketbase'
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
  schedule: '',
  portals: [] as string[],
})

const portals = ref<Portal[]>([])
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('access_groups', { code: form.value.code.trim() }))

async function loadOptions() {
  try {
    const [pts, scheds] = await Promise.all([
      pb.collection('portals').getFullList<Portal>({ sort: 'code' }),
      pb.collection('schedules').getFullList<Schedule>({ sort: 'code' }),
    ])
    portals.value = pts
    schedules.value = scheds
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const g = await pb.collection('access_groups').getOne<AccessGroup>(recordId)
    form.value = {
      code: g.code || '',
      name: g.name || '',
      schedule: g.schedule || '',
      portals: [...(g.portals || [])],
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load access group')
    router.push('/access-groups')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.schedule) { toast.error('Schedule is required'); return }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      schedule: form.value.schedule,
      portals: form.value.portals,
    }
    if (isEdit.value) {
      await pb.collection('access_groups').update(recordId!, data)
      toast.success('Access group updated')
      router.push(`/access-groups/${recordId}`)
    } else {
      const created = await pb.collection('access_groups').create<AccessGroup>(data)
      toast.success('Access group created')
      router.push(`/access-groups/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save access group')
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
      :title="isEdit ? 'Edit Access Group' : 'New Access Group'"
      :breadcrumbs="[{ label: 'Access Groups', to: '/access-groups' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Access Group">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required>
              <input v-model="form.code" type="text" placeholder="lobby-group" class="input input-bordered font-mono" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Lobby Access" class="input input-bordered" />
            </FormField>
          </div>

          <FormField label="Schedule" required>
            <select v-model="form.schedule" class="select select-bordered" required>
              <option value="">Select a schedule...</option>
              <option v-for="s in schedules" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
            </select>
            <p v-if="schedules.length === 0" class="text-xs text-warning">No schedules exist yet — create one first.</p>
          </FormField>
        </div>
      </BaseCard>

      <BaseCard title="Portals">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">The portals this group grants (during the schedule's windows).</p>
          <div class="border border-base-300 rounded-box p-3 max-h-64 overflow-y-auto space-y-1">
            <label v-for="p in portals" :key="p.id" class="flex items-center gap-3 cursor-pointer py-1 px-1 rounded hover:bg-base-200">
              <input type="checkbox" class="checkbox checkbox-sm" :value="p.id" v-model="form.portals" />
              <code class="text-sm font-medium">{{ p.code }}</code>
              <span class="text-sm opacity-50 truncate">{{ p.name }}</span>
            </label>
            <p v-if="portals.length === 0" class="text-sm opacity-50 py-2">No portals available. Create some first.</p>
          </div>
          <p class="text-xs opacity-50">{{ form.portals.length }} selected</p>
        </div>
      </BaseCard>

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">group.&lt;code&gt;</code>
          <p class="text-xs opacity-50">The controller mirrors this group to the ACC_POLICY bucket under this key.</p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Access Group</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
