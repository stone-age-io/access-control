<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { formatConstant } from '@/utils/format'
import { toCsv, downloadCsv, fileStamp, type CsvColumn } from '@/utils/csv'
import type { Location, Controller, Portal, AuxInput, AuxOutput, ControllerModel } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

// As-built / commissioning reference: every door's physical binding, grouped by
// location. The logical indices (relay/input) map to physical lines via the
// controller model's board profile; the contact-sense and lock-type fields are
// the install-time wiring decisions. This is the artifact an integrator hands over
// at the end of a job, so it must be exportable and print-friendly.

const loading = ref(false)
const error = ref('')
const locations = ref<Location[]>([])
const controllers = ref<Controller[]>([])
const portals = ref<Portal[]>([])
const auxInputs = ref<AuxInput[]>([])
const auxOutputs = ref<AuxOutput[]>([])

const ctrlById = computed(() => new Map(controllers.value.map((c) => [c.id, c])))

// Model → physical transport (see the board profiles in internal/drivers/hardware).
const MODEL_LABEL: Record<ControllerModel, string> = {
  'kincony-server-mini': 'GPIO · CM4',
  'kincony-pi5r8': 'I²C / MCP23017 · CM5',
}
function modelLabel(m: ControllerModel | ''): string {
  return m ? MODEL_LABEL[m] || m : '—'
}
function ctrlCode(id: string): string {
  return id ? ctrlById.value.get(id)?.code || '(unknown)' : '— unassigned'
}
function readerAddr(n: number): string {
  return n >= 0 ? String(n) : 'NATS only'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [locs, ctrls, ports, ins, outs] = await Promise.all([
      pb.collection('locations').getFullList<Location>({ batch: 500, sort: 'code' }),
      pb.collection('controllers').getFullList<Controller>({ batch: 500, sort: 'code' }),
      pb.collection('portals').getFullList<Portal>({ batch: 500, sort: 'code' }),
      pb.collection('aux_input').getFullList<AuxInput>({ batch: 500, sort: 'code' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ batch: 500, sort: 'code' }),
    ])
    locations.value = locs
    controllers.value = ctrls
    portals.value = ports
    auxInputs.value = ins
    auxOutputs.value = outs
  } catch (err: any) {
    error.value = err?.message || 'Failed to load topology'
  } finally {
    loading.value = false
  }
}

interface LocGroup {
  loc: Location
  controllers: Controller[]
  portals: Portal[]
  auxInputs: AuxInput[]
  auxOutputs: AuxOutput[]
}

const groups = computed<LocGroup[]>(() =>
  locations.value.map((loc) => ({
    loc,
    controllers: controllers.value.filter((c) => c.location === loc.id),
    portals: portals.value.filter((p) => p.location === loc.id),
    auxInputs: auxInputs.value.filter((a) => a.location === loc.id),
    auxOutputs: auxOutputs.value.filter((a) => a.location === loc.id),
  })),
)

interface WiringRow {
  location: string
  portal_code: string
  name: string
  type: string
  controller: string
  model: string
  lock_relay: number
  dps_input: number
  rex_input: number
  lock_type: string
  dps_contact: string
  rex_contact: string
  rex_unlock: string
  held_open_seconds: number
  pulse_seconds: number
  reader_address: number
}

const EXPORT_COLUMNS: CsvColumn<WiringRow>[] = [
  { key: 'location', label: 'Location' },
  { key: 'portal_code', label: 'Portal Code' },
  { key: 'name', label: 'Name' },
  { key: 'type', label: 'Type' },
  { key: 'controller', label: 'Controller' },
  { key: 'model', label: 'Model' },
  { key: 'lock_relay', label: 'Lock Relay' },
  { key: 'dps_input', label: 'DPS Input' },
  { key: 'rex_input', label: 'REX Input' },
  { key: 'lock_type', label: 'Lock Type' },
  { key: 'dps_contact', label: 'DPS Contact' },
  { key: 'rex_contact', label: 'REX Contact' },
  { key: 'rex_unlock', label: 'REX Unlock' },
  { key: 'held_open_seconds', label: 'Held Open (s)' },
  { key: 'pulse_seconds', label: 'Pulse (s)' },
  { key: 'reader_address', label: 'Reader Address' },
]

function exportCsv() {
  const rows: WiringRow[] = []
  for (const g of groups.value) {
    for (const p of g.portals) {
      const ctrl = ctrlById.value.get(p.controller)
      rows.push({
        location: g.loc.code,
        portal_code: p.code,
        name: p.name,
        type: p.type || '',
        controller: ctrl?.code || '',
        model: ctrl?.model || '',
        lock_relay: p.lock_relay,
        dps_input: p.dps_input,
        rex_input: p.rex_input,
        lock_type: p.lock_type || 'strike',
        dps_contact: p.dps_contact || 'nc',
        rex_contact: p.rex_contact || 'no',
        rex_unlock: p.rex_unlock ? 'yes' : 'no',
        held_open_seconds: p.held_open_seconds,
        pulse_seconds: p.pulse_seconds,
        reader_address: p.reader_address,
      })
    }
  }
  downloadCsv(`wiring-as-built-${fileStamp()}.csv`, toCsv(rows, EXPORT_COLUMNS))
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <div class="flex justify-between items-center gap-3">
      <p class="text-sm opacity-60">Physical bindings per location — logical indices map to board lines via each controller's model profile.</p>
      <button class="btn shrink-0" :disabled="loading || portals.length === 0" @click="exportCsv">Export portals CSV</button>
    </div>

    <div v-if="error" class="alert alert-error">
      <span>{{ error }}</span>
      <button class="btn btn-ghost btn-xs" @click="load">Retry</button>
    </div>

    <div v-if="loading" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div v-else-if="groups.length === 0" class="text-center py-12 opacity-50">
      <span class="text-3xl">🔧</span>
      <p class="text-sm mt-2">No locations configured yet.</p>
    </div>

    <template v-else>
      <BaseCard v-for="g in groups" :key="g.loc.id" :no-padding="true">
        <div class="px-4 py-3 border-b border-base-200">
          <div class="font-semibold">{{ g.loc.name || g.loc.code }}</div>
          <div class="text-xs opacity-50">{{ g.loc.code }} · {{ g.loc.timezone || 'no timezone' }} · {{ g.portals.length }} portals · {{ g.controllers.length }} controllers</div>
        </div>

        <!-- Controllers -->
        <div class="px-4 pt-3">
          <div class="text-xs uppercase tracking-wide opacity-50 mb-1">Controllers</div>
          <div v-if="g.controllers.length === 0" class="text-sm opacity-40 pb-2">None.</div>
          <div v-else class="overflow-x-auto">
            <table class="table table-xs">
              <thead><tr><th>Code</th><th>Name</th><th>Model</th></tr></thead>
              <tbody>
                <tr v-for="c in g.controllers" :key="c.id">
                  <td class="font-mono">{{ c.code }}</td>
                  <td>{{ c.name }}</td>
                  <td class="text-xs opacity-70">{{ modelLabel(c.model) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Portals -->
        <div class="px-4 pt-3">
          <div class="text-xs uppercase tracking-wide opacity-50 mb-1">Portals</div>
          <div v-if="g.portals.length === 0" class="text-sm opacity-40 pb-2">None.</div>
          <div v-else class="overflow-x-auto">
            <table class="table table-xs">
              <thead>
                <tr>
                  <th>Code</th><th>Type</th><th>Controller</th>
                  <th>Lock relay</th><th>DPS in</th><th>REX in</th>
                  <th>Lock type</th><th>DPS</th><th>REX</th><th>REX unlock</th>
                  <th>Held (s)</th><th>Pulse (s)</th><th>Reader</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="p in g.portals" :key="p.id">
                  <td><div class="font-mono">{{ p.code }}</div><div class="opacity-50">{{ p.name }}</div></td>
                  <td>{{ formatConstant(p.type || '') || '—' }}</td>
                  <td class="font-mono text-xs">{{ ctrlCode(p.controller) }}</td>
                  <td class="tabular-nums">{{ p.lock_relay }}</td>
                  <td class="tabular-nums">{{ p.dps_input }}</td>
                  <td class="tabular-nums">{{ p.rex_input }}</td>
                  <td>{{ p.lock_type || 'strike' }}</td>
                  <td class="uppercase">{{ p.dps_contact || 'nc' }}</td>
                  <td class="uppercase">{{ p.rex_contact || 'no' }}</td>
                  <td>{{ p.rex_unlock ? 'yes' : 'no' }}</td>
                  <td class="tabular-nums">{{ p.held_open_seconds }}</td>
                  <td class="tabular-nums">{{ p.pulse_seconds }}</td>
                  <td class="text-xs">{{ readerAddr(p.reader_address) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <!-- Aux I/O -->
        <div v-if="g.auxInputs.length || g.auxOutputs.length" class="px-4 pt-3 pb-4 grid grid-cols-1 lg:grid-cols-2 gap-4">
          <div v-if="g.auxInputs.length">
            <div class="text-xs uppercase tracking-wide opacity-50 mb-1">Aux inputs</div>
            <div class="overflow-x-auto">
              <table class="table table-xs">
                <thead><tr><th>Code</th><th>Controller</th><th>Input</th><th>Sense</th><th>Type</th></tr></thead>
                <tbody>
                  <tr v-for="a in g.auxInputs" :key="a.id">
                    <td class="font-mono">{{ a.code }}</td>
                    <td class="font-mono text-xs">{{ ctrlCode(a.controller) }}</td>
                    <td class="tabular-nums">{{ a.input_index }}</td>
                    <td class="uppercase">{{ a.contact || 'no' }}</td>
                    <td>{{ formatConstant(a.point_type || 'monitor') }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
          <div v-if="g.auxOutputs.length">
            <div class="text-xs uppercase tracking-wide opacity-50 mb-1">Aux outputs</div>
            <div class="overflow-x-auto">
              <table class="table table-xs">
                <thead><tr><th>Code</th><th>Controller</th><th>Relay</th><th>Pulse (s)</th></tr></thead>
                <tbody>
                  <tr v-for="a in g.auxOutputs" :key="a.id">
                    <td class="font-mono">{{ a.code }}</td>
                    <td class="font-mono text-xs">{{ ctrlCode(a.controller) }}</td>
                    <td class="tabular-nums">{{ a.relay_index }}</td>
                    <td class="tabular-nums">{{ a.pulse_seconds }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
        <div v-else class="pb-2"></div>
      </BaseCard>
    </template>
  </div>
</template>
