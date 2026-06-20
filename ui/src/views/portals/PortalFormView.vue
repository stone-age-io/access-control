<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Portal, Location, Controller, Schedule, Posture, PortalType } from '@/types/pocketbase'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import RailCard from '@/components/ui/RailCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const TYPES: PortalType[] = ['door', 'turnstile', 'elevator', 'gate', 'logical']
const POSTURES: { value: Posture; label: string }[] = [
  { value: 'secure', label: 'Secure (validate every tap)' },
  { value: 'free_access', label: 'Free access (any tap, no validation)' },
  { value: 'unlocked', label: 'Unlocked (held open, free passage)' },
  { value: 'lockdown', label: 'Lockdown (deny all)' },
  { value: 'disabled', label: 'Disabled (deny all)' },
]

const form = ref({
  code: '',
  name: '',
  type: 'door' as PortalType,
  location: '',
  posture: 'secure' as Posture,
  pulse_seconds: 5,
  controller: '',
  lock_relay: 0,
  dps_input: 0,
  rex_input: 0,
  held_open_seconds: 30,
  auto_posture: '' as Posture | '',
  auto_schedule: '',
})

const locations = ref<Location[]>([])
const controllers = ref<Controller[]>([])
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('portals', { code: form.value.code.trim() }))

async function loadOptions() {
  try {
    const [locs, ctrls, scheds] = await Promise.all([
      pb.collection('locations').getFullList<Location>({ sort: 'code' }),
      pb.collection('controllers').getFullList<Controller>({ sort: 'code' }),
      pb.collection('schedules').getFullList<Schedule>({ sort: 'code' }),
    ])
    locations.value = locs
    controllers.value = ctrls
    schedules.value = scheds
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const p = await pb.collection('portals').getOne<Portal>(recordId)
    form.value = {
      code: p.code || '',
      name: p.name || '',
      type: (p.type || 'door') as PortalType,
      location: p.location || '',
      posture: (p.posture || 'secure') as Posture,
      pulse_seconds: p.pulse_seconds || 5,
      controller: p.controller || '',
      lock_relay: p.lock_relay || 0,
      dps_input: p.dps_input || 0,
      rex_input: p.rex_input || 0,
      held_open_seconds: p.held_open_seconds || 0,
      auto_posture: (p.auto_posture || '') as Posture | '',
      auto_schedule: p.auto_schedule || '',
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load portal')
    router.push('/portals')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.type) { toast.error('Type is required'); return }
  if (!form.value.location) { toast.error('Location is required'); return }
  // Scheduled posture is both-or-neither: one set requires the other.
  if (form.value.auto_posture && !form.value.auto_schedule) {
    toast.error('Scheduled posture: pick a schedule, or clear the posture'); return
  }
  if (form.value.auto_schedule && !form.value.auto_posture) {
    toast.error('Scheduled posture: pick a posture, or clear the schedule'); return
  }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      type: form.value.type,
      location: form.value.location,
      posture: form.value.posture,
      pulse_seconds: Number(form.value.pulse_seconds) || 0,
      controller: form.value.controller,
      lock_relay: Number(form.value.lock_relay) || 0,
      dps_input: Number(form.value.dps_input) || 0,
      rex_input: Number(form.value.rex_input) || 0,
      held_open_seconds: Number(form.value.held_open_seconds) || 0,
      auto_posture: form.value.auto_posture,
      auto_schedule: form.value.auto_schedule,
    }
    if (isEdit.value) {
      await pb.collection('portals').update(recordId!, data)
      toast.success('Portal updated')
      router.push(`/portals/${recordId}`)
    } else {
      const created = await pb.collection('portals').create<Portal>(data)
      toast.success('Portal created')
      router.push(`/portals/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save portal')
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
      :title="isEdit ? 'Edit Portal' : 'New Portal'"
      :breadcrumbs="[{ label: 'Portals', to: '/portals' }, { label: isEdit ? 'Edit' : 'New' }]"
    >
      <BaseCard title="Portal">
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

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Type *</span></label>
              <select v-model="form.type" class="select select-bordered" required>
                <option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option>
              </select>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Location *</span></label>
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
              <label v-if="locations.length === 0" class="label">
                <span class="label-text-alt text-warning">No locations exist yet — create one first.</span>
              </label>
            </div>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Standing Posture</span></label>
              <select v-model="form.posture" class="select select-bordered">
                <option v-for="p in POSTURES" :key="p.value" :value="p.value">{{ p.label }}</option>
              </select>
              <label class="label"><span class="label-text-alt">Default state; a runtime command or scheduled posture can override it on the controller.</span></label>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Pulse (seconds)</span></label>
              <input v-model.number="form.pulse_seconds" type="number" min="0" class="input input-bordered" />
              <label class="label"><span class="label-text-alt">How long the lock releases on a grant.</span></label>
            </div>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Controller &amp; hardware">
        <div class="space-y-4">
          <div class="form-control">
            <label class="label"><span class="label-text">Controller</span></label>
            <select v-model="form.controller" class="select select-bordered">
              <option value="">Unassigned</option>
              <option v-for="c in controllers" :key="c.id" :value="c.id">{{ c.code }} — {{ c.name || c.code }}</option>
            </select>
            <label class="label">
              <span class="label-text-alt">The edge box that drives this portal. Unassigned portals (e.g. logical) are not armed by any box.</span>
            </label>
          </div>

          <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Lock relay</span></label>
              <input v-model.number="form.lock_relay" type="number" min="0" class="input input-bordered" />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">DPS input</span></label>
              <input v-model.number="form.dps_input" type="number" min="0" class="input input-bordered" />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">REX input</span></label>
              <input v-model.number="form.rex_input" type="number" min="0" class="input input-bordered" />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Held-open (s)</span></label>
              <input v-model.number="form.held_open_seconds" type="number" min="0" class="input input-bordered" />
            </div>
          </div>
          <p class="text-xs opacity-50">
            Logical relay/input indices on the controller; its model template maps them to physical lines.
            Door-position (DPS) and request-to-exit (REX) drive forced/held-open detection. Ignored for logical portals.
          </p>
        </div>
      </BaseCard>

      <BaseCard title="Scheduled posture">
        <div class="space-y-4">
          <p class="text-sm text-base-content/60">
            While the schedule's window is open, the controller adopts this posture instead of the standing one
            (a runtime command still overrides both). Set both, or leave both blank for no automation.
          </p>
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Posture</span></label>
              <select v-model="form.auto_posture" class="select select-bordered">
                <option value="">— None —</option>
                <option v-for="p in POSTURES" :key="p.value" :value="p.value">{{ p.label }}</option>
              </select>
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Schedule</span></label>
              <select v-model="form.auto_schedule" class="select select-bordered">
                <option value="">— None —</option>
                <option v-for="s in schedules" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
              </select>
              <label v-if="schedules.length === 0" class="label">
                <span class="label-text-alt text-warning">No schedules exist yet — create one first.</span>
              </label>
            </div>
          </div>
        </div>
      </BaseCard>

      <template #rail>
        <RailCard title="Policy KV key" icon="🔑">
          <code v-if="kvKey" class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
          <code v-else class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block opacity-60">portal.&lt;code&gt;</code>
          <p class="text-xs opacity-50">The controller looks up this portal by this key when a credential is presented.</p>
        </RailCard>
      </template>

      <template #footer>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Portal</span>
        </button>
      </template>
    </DetailLayout>
  </form>
</template>
