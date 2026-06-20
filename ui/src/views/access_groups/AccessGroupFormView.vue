<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { AccessGroup, Portal, Schedule } from '@/types/pocketbase'
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
  schedule: '',
  portals: [] as string[],
})

const portals = ref<Portal[]>([])
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('access_groups', { code: form.value.code.trim() }))

const portalLocation = (p: Portal) => p.expand?.location?.code || '—'
const portalSearch = (p: Portal) => [p.code, p.name, p.expand?.location?.code].filter(Boolean).join(' ')

async function loadOptions() {
  try {
    const [pts, scheds] = await Promise.all([
      pb.collection('portals').getFullList<Portal>({ sort: 'code', expand: 'location' }),
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
    <FormLayout
      :title="isEdit ? 'Edit Access Group' : 'New Access Group'"
      :breadcrumbs="[{ label: 'Access Groups', to: '/access-groups' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'group.<code>'"
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
          <p class="text-sm text-base-content/60">The portals this group grants (during the schedule's windows), grouped by location.</p>
          <RelationPicker
            v-model="form.portals"
            :options="portals"
            :group="portalLocation"
            :search-text="portalSearch"
            :primary="(p) => p.code"
            :secondary="(p) => p.name"
            empty="No portals available. Create some first."
          />
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Access Group</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
