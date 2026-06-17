<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { AccessPoint, Site, Posture } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const POSTURES: Posture[] = ['secure', 'unlocked', 'lockdown', 'disabled']

const form = ref({
  code: '',
  name: '',
  site: '',
  posture: 'secure' as Posture,
  pulse_seconds: 5,
})

const sites = ref<Site[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('access_points', { code: form.value.code.trim() }))

async function loadOptions() {
  try {
    sites.value = await pb.collection('sites').getFullList<Site>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load sites')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const p = await pb.collection('access_points').getOne<AccessPoint>(recordId)
    form.value = {
      code: p.code || '',
      name: p.name || '',
      site: p.site || '',
      posture: (p.posture || 'secure') as Posture,
      pulse_seconds: p.pulse_seconds || 5,
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load access point')
    router.push('/access-points')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.site) { toast.error('Site is required'); return }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      site: form.value.site,
      posture: form.value.posture,
      pulse_seconds: Number(form.value.pulse_seconds) || 0,
    }
    if (isEdit.value) {
      await pb.collection('access_points').update(recordId!, data)
      toast.success('Access point updated')
      router.push(`/access-points/${recordId}`)
    } else {
      const created = await pb.collection('access_points').create<AccessPoint>(data)
      toast.success('Access point created')
      router.push(`/access-points/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save access point')
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
      :title="isEdit ? 'Edit Access Point' : 'New Access Point'"
      :breadcrumbs="[{ label: 'Access Points', to: '/access-points' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Access Point">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Code *</span></label>
              <input v-model="form.code" type="text" placeholder="lobby-main" class="input input-bordered font-mono" required />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Name</span></label>
              <input v-model="form.name" type="text" placeholder="Main Lobby Door" class="input input-bordered" />
            </div>
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Site *</span></label>
            <select v-model="form.site" class="select select-bordered" required>
              <option value="">Select a site...</option>
              <option v-for="s in sites" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
            </select>
            <label v-if="sites.length === 0" class="label">
              <span class="label-text-alt text-warning">No sites exist yet — create one first.</span>
            </label>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Standing Posture</span></label>
              <select v-model="form.posture" class="select select-bordered">
                <option v-for="p in POSTURES" :key="p" :value="p">{{ p }}</option>
              </select>
              <label class="label"><span class="label-text-alt">Default state; a runtime command can override it on the controller.</span></label>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Pulse (seconds)</span></label>
              <input v-model.number="form.pulse_seconds" type="number" min="0" class="input input-bordered" />
              <label class="label"><span class="label-text-alt">How long the lock releases on a grant.</span></label>
            </div>
          </div>
        </div>
      </BaseCard>

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">point.&lt;code&gt;</code>
          <p class="text-xs opacity-50">The controller looks up this point by this key when a credential is presented.</p>
        </RailCard>
        <RailCard title="About access points" icon="🚪">
          <p class="text-xs opacity-60 leading-relaxed">
            A controllable opening — door, gate, turnstile, or elevator. Its standing posture is the default;
            a controller command can override it at runtime.
          </p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Access Point</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
