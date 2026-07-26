<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { badgePb } from '@/utils/badgePb'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import BadgeFloorplan from './BadgeFloorplan.vue'
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
 */
const props = defineProps<{ me: BadgeMe }>()
const emit = defineEmits<{ refresh: [] }>()

// Per-target in-flight + result state, keyed by record id, so one target's outcome
// never overwrites another's.
const busy = ref<Record<string, boolean>>({})
const results = ref<Record<string, { ok: boolean; message: string }>>({})

const remotePortals = computed<BadgePortal[]>(() => props.me.portals.filter((p) => p.remoteUnlock))
const viewOnlyPortals = computed<BadgePortal[]>(() => props.me.portals.filter((p) => !p.remoteUnlock))
const remoteAreas = computed<BadgeArea[]>(() => props.me.areas.filter((a) => a.remote))
const onSiteAreas = computed<BadgeArea[]>(() => props.me.areas.filter((a) => !a.remote))
const remoteOutputs = computed<BadgeOutput[]>(() => props.me.outputs.filter((o) => o.remote))
const onSiteOutputs = computed<BadgeOutput[]>(() => props.me.outputs.filter((o) => !o.remote))

/** Only a valid pass may drive anything. Everything else is read-only. */
const passUsable = computed(() => props.me.passState === 'valid')

const nothingGranted = computed(
  () => !props.me.portals.length && !props.me.areas.length && !props.me.outputs.length,
)

// --- floor plans -----------------------------------------------------------
//
// A separate, best-effort request: the list above is the complete surface, and a plan is
// an upgrade a location opts into (`locations.badge_floorplan`). Most installs return an
// empty list, so a failure here is silent — there is nothing to tell the holder about a
// picture they were never promised.
const plans = ref<BadgeLive['locations']>([])
const showPlan = ref(true)

onMounted(async () => {
  try {
    const res = await badgePb.send<BadgeLive>('/api/badge/live', { method: 'GET' })
    plans.value = res.locations || []
  } catch {
    plans.value = []
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
  <div class="space-y-4">
    <div v-if="!passUsable && !nothingGranted" class="alert alert-warning py-2 text-sm">
      <span>Your pass is not currently valid, so nothing here can be used yet.</span>
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
        :enabled="passUsable"
      />
    </template>

    <!-- Doors -->
    <div v-if="remotePortals.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3">
        <h2 class="card-title text-base">Open a door</h2>
        <div v-for="p in remotePortals" :key="p.id" class="space-y-1">
          <button
            class="btn btn-primary w-full justify-between"
            :disabled="busy[p.id] || !passUsable"
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

    <!-- Areas. Arm and disarm are separate rights, so they are separate buttons and
         either may be absent. -->
    <div v-if="remoteAreas.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3">
        <h2 class="card-title text-base">Areas</h2>
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
              :disabled="busy[a.id] || !passUsable"
              @click="disarm(a)"
            >
              Disarm
            </button>
            <button
              v-if="a.canArm"
              class="btn btn-sm btn-primary flex-1"
              :disabled="busy[a.id] || !passUsable"
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

    <!-- Aux outputs: one momentary action each. -->
    <div v-if="remoteOutputs.length" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3">
        <h2 class="card-title text-base">Controls</h2>
        <div v-for="o in remoteOutputs" :key="o.id" class="space-y-1">
          <button
            class="btn btn-outline w-full justify-between"
            :disabled="busy[o.id] || !passUsable"
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

    <!-- Everything on the badge that can only be used in person. Listed rather than
         hidden: the grant is real, and a holder who could not see it would think their
         badge does less than it does. -->
    <div
      v-if="viewOnlyPortals.length || onSiteAreas.length || onSiteOutputs.length"
      class="card bg-base-100 shadow-sm"
    >
      <div class="card-body gap-2">
        <h2 class="card-title text-base">On site only</h2>
        <p class="text-xs text-base-content/60">
          {{
            passUsable
              ? 'Present your badge, or use the keypad, at these.'
              : 'These are on your badge but need a valid pass.'
          }}
        </p>
        <ul class="text-sm space-y-1">
          <li v-for="p in viewOnlyPortals" :key="p.id" class="flex justify-between gap-2">
            <span>{{ p.name }}</span>
            <span class="text-base-content/50 text-xs">{{ p.location }}</span>
          </li>
          <li v-for="a in onSiteAreas" :key="a.id" class="flex justify-between gap-2">
            <span>{{ a.name }} <span class="opacity-50 text-xs">· area</span></span>
            <span class="text-base-content/50 text-xs">{{ a.location }}</span>
          </li>
          <li v-for="o in onSiteOutputs" :key="o.id" class="flex justify-between gap-2">
            <span>{{ o.name }} <span class="opacity-50 text-xs">· control</span></span>
            <span class="text-base-content/50 text-xs">{{ o.location }}</span>
          </li>
        </ul>
      </div>
    </div>

    <div v-if="nothingGranted" class="text-center text-sm text-base-content/50 py-8">
      Nothing is assigned to your badge yet.
    </div>
  </div>
</template>
