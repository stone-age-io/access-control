<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { badgePb } from '@/utils/badgePb'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import BadgeFloorplan from './BadgeFloorplan.vue'
import BadgeOnSiteList, { type OnSiteItem } from './BadgeOnSiteList.vue'
import { actionErrorText } from './reasonText'
import type { BadgeArea, BadgeLive, BadgeMe, BadgeOutput, BadgePortal } from '@/types/badge'

/**
 * What this badge can DO: doors, areas, and aux outputs.
 *
 * Every action posts to /api/badge/* and is authorized server-side by the same pure
 * deciders the edge runs (policy.Decide / DecideArea / DecideOutput) over a live
 * snapshot of the policy graph. Nothing here is a permission check — the buttons only
 * reflect what the server already said this badge holds, and pressing one the server
 * refuses produces a message, not an inconsistency.
 *
 * # readonly
 *
 * Set by the operator's badge preview (GET /api/badge/preview/{id}), which renders this
 * exact component so that what an operator sees while troubleshooting is what the holder
 * sees. In that mode the buttons are inert: the preview mints no badge session, so there is
 * nothing to act WITH — and deliberately so, since a badge action stamps the cardholder as
 * its actor, and an operator acting through a borrowed session would be indistinguishable
 * from the holder in the audit trail.
 */
const props = withDefaults(
  defineProps<{
    me: BadgeMe
    /**
     * Floor plans supplied by the caller. The holder's own device leaves this undefined and
     * the panel fetches /api/badge/live itself; the operator preview passes the plans it
     * already has, since a badge token is the only thing that route accepts.
     */
    plans?: BadgeLive['locations']
    readonly?: boolean
  }>(),
  { plans: undefined, readonly: false },
)
const emit = defineEmits<{ refresh: [] }>()

// Per-target in-flight + result state, keyed by record id, so one target's outcome
// never overwrites another's.
const busy = ref<Record<string, boolean>>({})
const results = ref<Record<string, { ok: boolean; message: string }>>({})

const remotePortals = computed<BadgePortal[]>(() => props.me.portals.filter((p) => p.remoteUnlock))
const remoteAreas = computed<BadgeArea[]>(() => props.me.areas.filter((a) => a.remote))
const remoteOutputs = computed<BadgeOutput[]>(() => props.me.outputs.filter((o) => o.remote))

/**
 * Everything usable only in person, flattened into one list for BadgeOnSiteList to group by
 * location. Flattened rather than three lists because the holder's question is "what else
 * is on my badge at this building", not "which collection is it in".
 */
const onSiteItems = computed<OnSiteItem[]>(() => [
  ...props.me.portals
    .filter((p) => !p.remoteUnlock)
    .map((p): OnSiteItem => ({ id: p.id, name: p.name, location: p.location, kind: 'door' })),
  ...props.me.areas
    .filter((a) => !a.remote)
    .map((a): OnSiteItem => ({ id: a.id, name: a.name, location: a.location, kind: 'area' })),
  ...props.me.outputs
    .filter((o) => !o.remote)
    .map((o): OnSiteItem => ({ id: o.id, name: o.name, location: o.location, kind: 'control' })),
])

/** Only a valid pass may drive anything. Everything else is read-only. */
const passUsable = computed(() => props.me.passState === 'valid')
/** What actually enables a button: a usable pass AND a session that can act. */
const canAct = computed(() => passUsable.value && !props.readonly)

const nothingGranted = computed(
  () => !props.me.portals.length && !props.me.areas.length && !props.me.outputs.length,
)

// --- floor plans -----------------------------------------------------------
//
// A best-effort request when the caller supplied none: the list above is the complete
// surface, and a plan is an upgrade a location opts into (`locations.badge_floorplan`). Most
// installs return an empty list, so a failure here is silent — there is nothing to tell the
// holder about a picture they were never promised.
const fetchedPlans = ref<BadgeLive['locations']>([])
const plans = computed<BadgeLive['locations']>(() => props.plans ?? fetchedPlans.value)
const showPlan = ref(true)

onMounted(async () => {
  if (props.plans !== undefined) return // the caller already has them
  try {
    const res = await badgePb.send<BadgeLive>('/api/badge/live', { method: 'GET' })
    fetchedPlans.value = res.locations || []
  } catch {
    fetchedPlans.value = []
  }
})

/**
 * Run one badge action. `refresh` re-fetches the badge afterwards, which arming needs:
 * an area's state is server-resolved, so the only honest way to show the new state is to
 * ask again rather than assume the write landed as requested.
 */
async function act(id: string, path: string, successText: string, refresh = false) {
  busy.value = { ...busy.value, [id]: true }
  delete results.value[id]
  try {
    await badgePb.send(path, { method: 'POST' })
    results.value = { ...results.value, [id]: { ok: true, message: successText } }
    if (refresh) emit('refresh')
  } catch (err: any) {
    results.value = { ...results.value, [id]: { ok: false, message: actionErrorText(err) } }
  } finally {
    const next = { ...busy.value }
    delete next[id]
    busy.value = next
  }
}

const unlock = (p: BadgePortal) => act(p.id, `/api/badge/unlock/${p.id}`, 'Unlocked')
const arm = (a: BadgeArea) => act(a.id, `/api/badge/areas/${a.id}/arm`, 'Armed', true)
const disarm = (a: BadgeArea) => act(a.id, `/api/badge/areas/${a.id}/disarm`, 'Disarmed', true)
const pulse = (o: BadgeOutput) => act(o.id, `/api/badge/outputs/${o.id}/pulse`, 'Activated')

/** The state chip. `unknown` gets no colour: it is an absence of information. */
function stateTone(state: BadgeArea['state']) {
  if (state === 'armed') return 'warning'
  if (state === 'disarmed') return 'success'
  return 'neutral'
}
function stateLabel(state: BadgeArea['state']) {
  if (state === 'armed') return 'Armed'
  if (state === 'disarmed') return 'Disarmed'
  return 'State unknown'
}
</script>

<template>
  <div class="space-y-3">
    <div v-if="!passUsable && !nothingGranted" class="alert alert-warning py-2 text-sm">
      <span>
        {{
          readonly
            ? 'Their pass is not currently valid, so nothing here can be used.'
            : 'Your pass is not currently valid, so nothing here can be used yet.'
        }}
      </span>
    </div>

    <!-- Floor plans, when a location opted in. Shown above the lists because "which door
         am I at" is the question a plan answers better than a list ever does — and
         collapsible, because a list is faster once you know the building. -->
    <template v-if="plans.length">
      <div class="flex justify-end">
        <button class="btn btn-xs btn-ghost gap-1" @click="showPlan = !showPlan">
          {{ showPlan ? '☰ List only' : '🗺️ Show plan' }}
        </button>
      </div>
      <BadgeFloorplan
        v-for="plan in showPlan ? plans : []"
        :key="plan.id"
        :location="plan"
        :enabled="canAct"
        :disabled-note="readonly ? 'Read-only preview — open doors from Live View' : undefined"
      />
    </template>

    <!-- Doors. Bounded with its own scroll for the same reason as the on-site list: a badge
         with twenty remote doors must not turn this tab into a document. -->
    <div v-if="remotePortals.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <h2 class="card-title text-base">Open a door</h2>
        <div class="max-h-64 overflow-y-auto overscroll-contain space-y-2 -mx-1 px-1">
          <div v-for="p in remotePortals" :key="p.id" class="space-y-1">
            <button
              class="btn btn-primary w-full justify-between"
              :disabled="busy[p.id] || !canAct"
              @click="unlock(p)"
            >
              <span class="text-left">
                {{ p.name }}
                <span v-if="p.location" class="block text-xs font-normal opacity-70">{{ p.location }}</span>
              </span>
              <span v-if="busy[p.id]" class="loading loading-spinner loading-sm"></span>
            </button>
            <p
              v-if="results[p.id]"
              class="text-xs px-1"
              :class="results[p.id].ok ? 'text-success' : 'text-error'"
            >
              {{ results[p.id].message }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Areas. Arm and disarm are separate rights, so they are separate buttons and
         either may be absent. -->
    <div v-if="remoteAreas.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <h2 class="card-title text-base">Areas</h2>
        <div class="max-h-64 overflow-y-auto overscroll-contain space-y-3 -mx-1 px-1">
          <div v-for="a in remoteAreas" :key="a.id" class="space-y-2">
            <div class="flex items-center justify-between gap-2">
              <div class="min-w-0">
                <div class="font-medium text-sm truncate">{{ a.name }}</div>
                <div v-if="a.location" class="text-xs opacity-60">{{ a.location }}</div>
              </div>
              <SoftBadge :tone="stateTone(a.state)" dot>{{ stateLabel(a.state) }}</SoftBadge>
            </div>
            <div class="flex gap-2">
              <button
                v-if="a.canDisarm"
                class="btn btn-sm btn-outline flex-1"
                :disabled="busy[a.id] || !canAct"
                @click="disarm(a)"
              >
                Disarm
              </button>
              <button
                v-if="a.canArm"
                class="btn btn-sm btn-primary flex-1"
                :disabled="busy[a.id] || !canAct"
                @click="arm(a)"
              >
                Arm
              </button>
              <span v-if="busy[a.id]" class="loading loading-spinner loading-sm self-center"></span>
            </div>
            <p
              v-if="results[a.id]"
              class="text-xs px-1"
              :class="results[a.id].ok ? 'text-success' : 'text-error'"
            >
              {{ results[a.id].message }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Aux outputs: one momentary action each. -->
    <div v-if="remoteOutputs.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <h2 class="card-title text-base">Controls</h2>
        <div class="max-h-64 overflow-y-auto overscroll-contain space-y-2 -mx-1 px-1">
          <div v-for="o in remoteOutputs" :key="o.id" class="space-y-1">
            <button
              class="btn btn-outline w-full justify-between"
              :disabled="busy[o.id] || !canAct"
              @click="pulse(o)"
            >
              <span class="text-left">
                {{ o.name }}
                <span v-if="o.location" class="block text-xs font-normal opacity-70">{{ o.location }}</span>
              </span>
              <span v-if="busy[o.id]" class="loading loading-spinner loading-sm"></span>
            </button>
            <p
              v-if="results[o.id]"
              class="text-xs px-1"
              :class="results[o.id].ok ? 'text-success' : 'text-error'"
            >
              {{ results[o.id].message }}
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Everything on the badge that can only be used in person: grouped by building,
         collapsible, filterable, and bounded. See BadgeOnSiteList. -->
    <BadgeOnSiteList v-if="onSiteItems.length" :items="onSiteItems" :pass-usable="passUsable" />

    <div v-if="nothingGranted" class="text-center text-sm text-base-content/50 py-8">
      {{ readonly ? 'Nothing is assigned to this badge yet.' : 'Nothing is assigned to your badge yet.' }}
    </div>
  </div>
</template>
