<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { formatConstant, localInputToISO, isoToLocalInput } from '@/utils/format'
import { reasonExplanation } from '@/utils/events'
import type { Credential, Portal, Posture } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'
import Combobox from '@/components/ui/Combobox.vue'

// What-if tool: send (credential value, portal code, instant, optional posture
// override) to POST /api/simulate, which runs the REAL policy.Decide over a live
// snapshot of the policy KV — the same function and data the edge controller uses.
// Nothing is published or changed; this only reveals what policy already grants.

interface SimResult {
  allow: boolean
  reason: string
  user: string
  pulse: number
  posture: Posture | ''
  postureSource: 'standing' | 'scheduled' | 'override' | ''
  portalKnown: boolean
  credKnown: boolean
  location: string
  timezone: string
}

const credentials = ref<Credential[]>([])
const portals = ref<Portal[]>([])
const loadError = ref('')

const credential = ref('') // credential value
const portal = ref('') // portal code
const at = ref(isoToLocalInput(new Date()))
const override = ref<Posture | ''>('')

const running = ref(false)
const runError = ref('')
const result = ref<SimResult | null>(null)

const POSTURES: Posture[] = ['secure', 'free_access', 'unlocked', 'lockdown', 'disabled']

async function loadOptions() {
  loadError.value = ''
  try {
    const [creds, ports] = await Promise.all([
      pb.collection('credentials').getFullList<Credential>({ batch: 500, sort: 'value', expand: 'user' }),
      pb.collection('portals').getFullList<Portal>({ batch: 500, sort: 'code' }),
    ])
    credentials.value = creds
    portals.value = ports
  } catch (err: any) {
    loadError.value = err?.message || 'Failed to load credentials/portals'
  }
}

// Combobox accessors (typed here rather than inline so the generic component
// infers cleanly). Credential is keyed by its value; portal by its code.
const credValue = (c: Credential) => c.value
const credPrimary = (c: Credential) => c.value
function credSecondary(c: Credential): string {
  const who = c.expand?.user?.name || c.user || 'unknown'
  return c.status && c.status !== 'active' ? `${who} · ${c.status}` : who
}
const portalValue = (p: Portal) => p.code
const portalPrimary = (p: Portal) => p.code
const portalSecondary = (p: Portal) => p.name

async function run() {
  if (!portal.value) {
    runError.value = 'Pick a portal first.'
    return
  }
  running.value = true
  runError.value = ''
  try {
    const body: Record<string, unknown> = {
      credential: credential.value,
      portal: portal.value,
      at: localInputToISO(at.value),
    }
    if (override.value) body.posture = override.value
    result.value = await pb.send<SimResult>('/api/simulate', { method: 'POST', body })
  } catch (err: any) {
    runError.value = err?.message || 'Simulation failed'
    result.value = null
  } finally {
    running.value = false
  }
}

const explanation = computed(() => (result.value ? reasonExplanation(result.value.reason) : ''))

onMounted(loadOptions)
</script>

<template>
  <div class="space-y-4">
    <p class="text-sm opacity-60">
      Runs the real decision function over the live policy — answer "would this badge open this door at this time?"
      without sending anyone to tap a credential. Nothing is changed.
    </p>

    <div v-if="loadError" class="alert alert-error">
      <span>{{ loadError }}</span>
      <button class="btn btn-ghost btn-xs" @click="loadOptions">Retry</button>
    </div>

    <!-- overflow-visible so the combobox dropdowns aren't clipped by the card
         (DaisyUI's .card clips to its rounded corners). -->
    <BaseCard class="overflow-visible">
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div class="form-control">
          <span class="label-text mb-1">Credential <span class="opacity-50">(optional — leave empty to test posture only)</span></span>
          <Combobox
            v-model="credential"
            :options="credentials"
            :option-value="credValue"
            :primary="credPrimary"
            :secondary="credSecondary"
            placeholder="Search credentials by value or cardholder…"
          />
        </div>

        <div class="form-control">
          <span class="label-text mb-1">Portal</span>
          <Combobox
            v-model="portal"
            :options="portals"
            :option-value="portalValue"
            :primary="portalPrimary"
            :secondary="portalSecondary"
            placeholder="Search portals by code or name…"
            :clearable="false"
          />
        </div>

        <label class="form-control">
          <span class="label-text mb-1">At (local time)</span>
          <input v-model="at" type="datetime-local" class="input input-bordered" />
        </label>

        <label class="form-control">
          <span class="label-text mb-1">Posture override <span class="opacity-50">(what-if; optional)</span></span>
          <select v-model="override" class="select select-bordered">
            <option value="">Resolve normally (standing / scheduled)</option>
            <option v-for="p in POSTURES" :key="p" :value="p">{{ formatConstant(p) }}</option>
          </select>
        </label>
      </div>

      <div class="flex items-center gap-3 mt-4">
        <button class="btn btn-primary" :disabled="running || !portal" @click="run">
          <span v-if="running" class="loading loading-spinner loading-xs"></span>
          Simulate
        </button>
        <span v-if="runError" class="text-error text-sm">{{ runError }}</span>
      </div>
    </BaseCard>

    <!-- Result -->
    <BaseCard v-if="result" :class="result.allow ? 'border-success/50' : 'border-error/50'">
      <div class="flex items-center gap-3">
        <span class="text-3xl">{{ result.allow ? '✅' : '⛔' }}</span>
        <div>
          <div class="text-2xl font-bold" :class="result.allow ? 'text-success' : 'text-error'">
            {{ result.allow ? 'GRANT' : 'DENY' }}
          </div>
          <div class="font-mono text-sm opacity-70">{{ result.reason }}</div>
        </div>
      </div>

      <p v-if="explanation" class="text-sm mt-3">{{ explanation }}</p>

      <div class="divider my-3"></div>

      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
        <div>
          <div class="opacity-50 text-xs">Effective posture</div>
          <div class="font-medium">
            {{ result.posture ? formatConstant(result.posture) : '—' }}
            <span v-if="result.postureSource" class="badge badge-xs badge-ghost ml-1">{{ result.postureSource }}</span>
          </div>
        </div>
        <div>
          <div class="opacity-50 text-xs">Cardholder</div>
          <div class="font-medium">{{ result.user || '—' }}</div>
        </div>
        <div v-if="result.allow">
          <div class="opacity-50 text-xs">Strike pulse</div>
          <div class="font-medium">{{ result.pulse }}s</div>
        </div>
        <div>
          <div class="opacity-50 text-xs">Evaluated in</div>
          <div class="font-medium">{{ result.timezone || 'UTC' }}</div>
        </div>
      </div>

      <div v-if="!result.portalKnown || (credential && !result.credKnown)" class="alert alert-warning mt-3 text-sm">
        <span>
          <template v-if="!result.portalKnown">This portal code isn't in the synced policy. </template>
          <template v-if="credential && !result.credKnown">This credential isn't in the synced policy (not enrolled, or not yet mirrored to the edge).</template>
        </span>
      </div>
    </BaseCard>
  </div>
</template>
