<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { policyKey } from '@/utils/policyKey'
import type { Portal, Location, Controller, Schedule, Posture, PortalType } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import IndexPicker from '@/components/ui/IndexPicker.vue'
import { useControllerIO } from '@/composables/useControllerIO'
import { conflictsAt } from '@/utils/io'

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
  // Prefill the location when arriving from a location page (?location=<id>).
  location: (route.query.location as string) || '',
  posture: 'secure' as Posture,
  pulse_seconds: 5,
  // Prefill the controller when arriving from a controller page (?controller=<id>).
  controller: (route.query.controller as string) || '',
  lock_relay: 0,
  dps_input: 0,
  rex_input: 0,
  held_open_seconds: 30,
  // OSDP reader: off => NATS-only (reader_address -1); on => a physical reader at
  // reader_address (0..126) on the controller's RS485 bus. New portals default off.
  osdpEnabled: false,
  reader_address: 0,
  auto_posture: '' as Posture | '',
  auto_schedule: '',
})

const locations = ref<Location[]>([])
const controllers = ref<Controller[]>([])
const schedules = ref<Schedule[]>([])
const loading = ref(false)
const loadingRecord = ref(false)

const kvKey = computed(() => policyKey('portals', { code: form.value.code.trim() }))

// The assigned controller's hardware capacity + current I/O occupancy, so the
// relay/input pickers can offer valid indices and flag collisions on that box.
const ctrlId = computed(() => form.value.controller)
const { profile, io } = useControllerIO(ctrlId)
const relayLines = computed(() => profile.value?.relays ?? [])
const inputLines = computed(() => profile.value?.inputs ?? [])

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
      // reader_address >= 0 means a physical OSDP reader; -1 (or absent) is NATS-only.
      osdpEnabled: typeof p.reader_address === 'number' && p.reader_address >= 0,
      reader_address: typeof p.reader_address === 'number' && p.reader_address >= 0 ? p.reader_address : 0,
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
  // An OSDP reader needs a valid PD address (0..126); off means NATS-only.
  if (form.value.osdpEnabled) {
    const a = Number(form.value.reader_address)
    if (!Number.isInteger(a) || a < 0 || a > 126) {
      toast.error('OSDP reader address must be a whole number 0–126'); return
    }
  }
  // DPS and REX are distinct functions; they can't share one input line.
  if (form.value.dps_input && form.value.dps_input === form.value.rex_input) {
    toast.error('DPS and REX cannot use the same input index'); return
  }
  // Reject indices already claimed by another portal/aux point on the same box.
  const conflicts: string[] = []
  const lr = conflictsAt(io.value.relays, form.value.lock_relay, recordId)
  if (lr.length) conflicts.push(`lock relay ${form.value.lock_relay} (${lr.map((o) => o.label).join(', ')})`)
  const dps = conflictsAt(io.value.inputs, form.value.dps_input, recordId)
  if (dps.length) conflicts.push(`DPS input ${form.value.dps_input} (${dps.map((o) => o.label).join(', ')})`)
  const rex = conflictsAt(io.value.inputs, form.value.rex_input, recordId)
  if (rex.length) conflicts.push(`REX input ${form.value.rex_input} (${rex.map((o) => o.label).join(', ')})`)
  if (conflicts.length) {
    toast.error(`Hardware conflict on this controller: ${conflicts.join('; ')}`); return
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
      // -1 disables OSDP (NATS-only); otherwise the PD address on the RS485 bus.
      reader_address: form.value.osdpEnabled ? (Number(form.value.reader_address) || 0) : -1,
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
    <FormLayout
      :title="isEdit ? 'Edit Portal' : 'New Portal'"
      :breadcrumbs="[{ label: 'Portals', to: '/portals' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'portal.<code>'"
    >
      <BaseCard title="Portal">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required>
              <input v-model="form.code" type="text" placeholder="lobby-main" class="input input-bordered font-mono" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Main Lobby Door" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Type" required>
              <select v-model="form.type" class="select select-bordered" required>
                <option v-for="t in TYPES" :key="t" :value="t">{{ t }}</option>
              </select>
            </FormField>
            <FormField label="Location" required>
              <select v-model="form.location" class="select select-bordered" required>
                <option value="">Select a location...</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.code }} — {{ l.name || l.code }}</option>
              </select>
              <p v-if="locations.length === 0" class="text-xs text-warning">No locations exist yet — create one first.</p>
            </FormField>
          </div>

        </div>
      </BaseCard>

      <BaseCard title="Posture &amp; timing">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Standing posture" hint="Default state; a runtime command or scheduled posture can override it on the controller.">
              <select v-model="form.posture" class="select select-bordered">
                <option v-for="p in POSTURES" :key="p.value" :value="p.value">{{ p.label }}</option>
              </select>
            </FormField>
            <FormField label="Pulse (seconds)" hint="How long the lock releases on a grant.">
              <input v-model.number="form.pulse_seconds" type="number" min="0" class="input input-bordered" />
            </FormField>
          </div>

          <div class="border-t border-base-200 pt-4 space-y-4">
            <div>
              <div class="text-sm font-medium text-base-content/90">Scheduled override</div>
              <p class="text-sm text-base-content/60 mt-1">
                While the schedule's window is open, the controller adopts this posture instead of the standing one
                (a runtime command still overrides both). Set both, or leave both blank for no automation.
              </p>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <FormField label="Posture">
                <select v-model="form.auto_posture" class="select select-bordered">
                  <option value="">— None —</option>
                  <option v-for="p in POSTURES" :key="p.value" :value="p.value">{{ p.label }}</option>
                </select>
              </FormField>
              <FormField label="Schedule">
                <select v-model="form.auto_schedule" class="select select-bordered">
                  <option value="">— None —</option>
                  <option v-for="s in schedules" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
                </select>
                <p v-if="schedules.length === 0" class="text-xs text-warning">No schedules exist yet — create one first.</p>
              </FormField>
            </div>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Controller &amp; hardware">
        <div class="space-y-4">
          <FormField label="Controller" hint="The edge box that drives this portal. Unassigned portals (e.g. logical) are not armed by any box.">
            <select v-model="form.controller" class="select select-bordered">
              <option value="">Unassigned</option>
              <option v-for="c in controllers" :key="c.id" :value="c.id">{{ c.code }} — {{ c.name || c.code }}</option>
            </select>
          </FormField>

          <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
            <FormField label="Lock relay">
              <IndexPicker v-model="form.lock_relay" :lines="relayLines" :usage="io.relays" :self-id="recordId" none-label="— no lock —" />
            </FormField>
            <FormField label="DPS input">
              <IndexPicker v-model="form.dps_input" :lines="inputLines" :usage="io.inputs" :self-id="recordId" />
            </FormField>
            <FormField label="REX input">
              <IndexPicker v-model="form.rex_input" :lines="inputLines" :usage="io.inputs" :self-id="recordId" />
            </FormField>
          </div>
          <div class="grid grid-cols-2 md:grid-cols-3 gap-4">
            <FormField label="Held-open (s)">
              <input v-model.number="form.held_open_seconds" type="number" min="0" class="input input-bordered" />
            </FormField>
          </div>

          <div class="border-t border-base-200 pt-4 space-y-3">
            <FormField inline label="OSDP reader"
                       hint="On = a physical OSDP reader on the controller's RS485 bus. Off = NATS-only (taps published over NATS). The controller polls this reader only when its reader mode is “osdp” or “both”.">
              <input v-model="form.osdpEnabled" type="checkbox" class="toggle toggle-primary" />
            </FormField>
            <FormField v-if="form.osdpEnabled" label="Reader address (OSDP PD)" hint="PD address on the controller's RS485 bus, 0–126; a single-reader bus uses 0.">
              <input v-model.number="form.reader_address" type="number" min="0" max="126" class="input input-bordered md:w-48" />
            </FormField>
          </div>

          <p class="text-xs opacity-50">
            Relay/input pickers list the assigned controller model's lines and flag any already in use on that box.
            Door-position (DPS) and request-to-exit (REX) drive forced/held-open detection; leave at “none” if unmonitored. Ignored for logical portals.
            Pick a controller to see its lines (otherwise enter the raw index).
          </p>
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Portal</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>
