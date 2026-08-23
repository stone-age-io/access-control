<script setup lang="ts">
import { ref, computed } from 'vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import BadgeFloorplan from './BadgeFloorplan.vue'
import BadgeOnSiteList, { type OnSiteItem } from './BadgeOnSiteList.vue'
import BadgeFilterInput from './BadgeFilterInput.vue'
import BadgeActionNote from './BadgeActionNote.vue'
import { onSiteGrants, remoteGrants, type BadgeViewKey } from './badgeNav'
import { useBadgeAction } from './useBadgeAction'
import type { BadgeArea, BadgeLive, BadgeLiveLocation, BadgeMe, BadgeOutput, BadgePortal } from '@/types/badge'

/**
 * What this badge can DO: doors, areas, and aux outputs — one view at a time.
 *
 * Every action posts to /api/badge/* and is authorized server-side by the same pure
 * deciders the edge runs (policy.Decide / DecideArea / DecideOutput) over a live snapshot
 * of the policy graph. Nothing here is a permission check — the buttons only reflect what
 * the server already said this badge holds, and pressing one the server refuses produces a
 * message, not an inconsistency.
 *
 * # Why it does not own its own switcher
 *
 * It used to derive the available views and render a row of pills for them. Both hosts
 * needed that list for their own chrome — the holder's shell puts it in a bottom navigation
 * bar, the operator's preview in a row of dialog tabs — so the derivation moved to
 * `badgeNav.ts` and this became a pure renderer, told which view to draw.
 *
 * That split is what keeps the operator preview honest: the two switchers look nothing alike
 * and are computed from one function, so a segment the holder sees and the operator does not
 * is impossible rather than merely unlikely.
 *
 * # readonly
 *
 * Set by the operator's badge preview (GET /api/badge/preview/{id}), which renders this exact
 * component so that what an operator sees while troubleshooting is what the holder sees. In
 * that mode the buttons are inert: the preview mints no badge session, so there is nothing to
 * act WITH — and deliberately so, since a badge action stamps the cardholder as its actor, and
 * an operator acting through a borrowed session would be indistinguishable from the holder in
 * the audit trail.
 */
const props = withDefaults(
  defineProps<{
    me: BadgeMe
    /**
     * Floor plans, from GET /api/badge/live. Supplied by the caller rather than fetched here:
     * the holder's shell needs them to know whether the Plan segment exists at all, and the
     * operator preview already has them (a badge token is the only thing that route accepts).
     * Normally empty — a plan is an upgrade a location opts into.
     */
    plans: BadgeLive['locations']
    /** Which view to draw. Resolved by the host against `badgeViews()`. */
    view: BadgeViewKey
    readonly?: boolean
  }>(),
  { readonly: false },
)
const emit = defineEmits<{ refresh: [] }>()

// In-flight + outcome state, keyed by record id and self-expiring. See useBadgeAction for why
// an outcome must not outlive itself: a remote unlock is a momentary pulse, so a permanent
// "Unlocked" beside a door states something untrue about the world.
const { busy, results, run } = useBadgeAction()

// The same split `badgeNav` counts the segments from, so a segment's count can never
// disagree with the number of rows under it.
const remote = computed(() => remoteGrants(props.me))
const remotePortals = computed<BadgePortal[]>(() => remote.value.portals)
const remoteAreas = computed<BadgeArea[]>(() => remote.value.areas)
const remoteOutputs = computed<BadgeOutput[]>(() => remote.value.outputs)

/**
 * Everything usable only in person, flattened into one list for BadgeOnSiteList to group by
 * location. Flattened rather than three lists because the holder's question is "what else is
 * on my badge at this building", not "which collection is it in".
 */
const onSiteItems = computed<OnSiteItem[]>(() => {
  const g = onSiteGrants(props.me)
  return [
    ...g.portals.map((p): OnSiteItem => ({ id: p.id, name: p.name, location: p.location, kind: 'door' })),
    ...g.areas.map((a): OnSiteItem => ({ id: a.id, name: a.name, location: a.location, kind: 'area' })),
    ...g.outputs.map((o): OnSiteItem => ({ id: o.id, name: o.name, location: o.location, kind: 'control' })),
  ]
})

/** Only a valid pass may drive anything. Everything else is read-only. */
const passUsable = computed(() => props.me.passState === 'valid')
/** What actually enables a button: a usable pass AND a session that can act. */
const canAct = computed(() => passUsable.value && !props.readonly)

const nothingGranted = computed(
  () => !props.me.portals.length && !props.me.areas.length && !props.me.outputs.length,
)

// --- which site's plan is showing -------------------------------------------
//
// /api/badge/live returns every site the holder has something placed at, and this used to
// render all of them stacked. That is not wrong so much as invisible: each plan is a
// full-width image, so on a phone the first one plus its action bar is about a screen, and
// the second card's header starts below the fold with nothing to suggest it is there.
//
// So the plan view shows ONE site, chosen from a native select. A row of pills was the first
// attempt and it competed with the switcher it sat under; a select is one line whatever the
// site count, hands the choosing to the platform's own picker (a wheel on iOS, a sheet on
// Android), and above all gives the plan back the vertical space — the whole point of this
// view is the picture. Absent at one site.
//
// Rendering one at a time also settles something the stacked version got wrong for free:
// each BadgeFloorplan owns its own selection, so two plans on screen could each caption a
// selected door, and the one scrolled off the top kept its stale result text. Keying the
// component by site id means switching destroys and rebuilds it, so a selection can never
// outlive the plan it was made on.
const SITE_STORAGE_KEY = 'sa.badge.site'

function storedSite(): string {
  // Not in the operator's preview: this renders what the HOLDER sees, and a site picked
  // while troubleshooting must not follow the operator to their own badge.
  if (props.readonly) return ''
  return localStorage.getItem(SITE_STORAGE_KEY) || ''
}
const chosenSite = ref<string>(storedSite())

/**
 * The site actually rendered. Resolved rather than stored, so a stale choice needs no cleanup
 * anywhere: a site that opted out of badge plans, or one whose last granted door was removed,
 * simply stops matching and the first site takes over. Remembering is never worse than not —
 * the fallback is the same name-sorted first entry the server already returns.
 */
const activeSite = computed<BadgeLiveLocation | undefined>(() => {
  const available = props.plans
  return available.find((l) => l.id === chosenSite.value) ?? available[0]
})

function setSite(id: string) {
  chosenSite.value = id
  if (!props.readonly) localStorage.setItem(SITE_STORAGE_KEY, id)
}

// --- filtering the portal list ----------------------------------------------
//
// The same shape BadgeOnSiteList already uses, and offered on the same terms: past a handful
// of doors you know the name of the one you want, and below that a search box is chrome over
// a list you can read at a glance. Matched against the building name too, because "warehouse"
// is how a holder with four sites narrows to one — which is also why this stayed a filter
// rather than becoming a location picker: a picker set to one building would keep the other
// three's doors off the screen for as long as it stayed set, and this is the surface where a
// hidden door is a holder standing outside one.
//
// Deliberately NOT persisted, unlike the view and the plan's site. A filter is a thing you
// are doing right now; coming back to a badge that shows three of your twelve doors, because
// of something typed last week, is how a holder concludes their access was revoked.
const PORTAL_FILTER_THRESHOLD = 6

const portalQuery = ref('')
const showPortalFilter = computed(() => remotePortals.value.length >= PORTAL_FILTER_THRESHOLD)

const filteredPortals = computed<BadgePortal[]>(() => {
  const q = portalQuery.value.trim().toLowerCase()
  if (!q) return remotePortals.value
  return remotePortals.value.filter((p) => `${p.name} ${p.location}`.toLowerCase().includes(q))
})

// Arming asks the shell to re-fetch, and only on success: an area's state is resolved
// server-side, so the only honest way to show the new state is to ask again rather than assume
// the write landed as requested. The other three change nothing durable and need no reload.
const reload = () => emit('refresh')

const unlock = (p: BadgePortal) => run(p.id, `/api/badge/unlock/${p.id}`, 'Unlocked')
const arm = (a: BadgeArea) => run(a.id, `/api/badge/areas/${a.id}/arm`, 'Armed', reload)
const disarm = (a: BadgeArea) => run(a.id, `/api/badge/areas/${a.id}/disarm`, 'Disarmed', reload)
const pulse = (o: BadgeOutput) => run(o.id, `/api/badge/outputs/${o.id}/pulse`, 'Activated')

/**
 * The button's own colour while an outcome is showing — the confirmation you can take in
 * without reading, next to the sentence that says what happened.
 *
 * The LABEL deliberately does not change. It is the door's name, and swapping it for "Unlocked"
 * for a few seconds leaves the holder unsure which of six similar buttons they actually pressed
 * (and makes the control's accessible name change under a screen reader mid-action). Tinting
 * says the same thing without taking the name away.
 */
function toneClass(id: string, idle: string): string {
  const result = results.value[id]
  if (!result) return idle
  return result.ok ? 'btn-success' : 'btn-error'
}

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
  <!-- A flex column, not a stack of margins, because the plan view has to fill the height it
       is given: the shell hands this panel a definite height (see BadgeView's ONE scroll
       region), the rows above are `shrink-0`, and the plan card takes the rest. The list views
       leave the column's slack empty and overflow it when they are long, which the shell's
       scroll region carries. -->
  <div class="flex h-full flex-col gap-3">
    <div v-if="!passUsable && !nothingGranted" class="alert alert-warning shrink-0 py-2 text-sm">
      <span>
        {{
          readonly
            ? 'Their pass is not currently valid, so nothing here can be used.'
            : 'Your pass is not currently valid, so nothing here can be used yet.'
        }}
      </span>
    </div>

    <!-- Floor plans, when a location opted in. First in the navigation, because "which door
         am I at" is the question a plan answers better than a list ever does.

         `flex-1 min-h-0` is the whole viewport-filling story on this side: the panel is a flex
         column of known height (see the root), so the plan card takes the rest of it.

         The site picker goes in the card's HEADER slot rather than in a row above the card.
         Both name the location, so side by side they said it twice — "East Coast Office" as the
         card's heading and again as the select's chosen option directly above it. Putting the
         picker where the heading goes makes it the answer to "which building", which is what it
         already was, and gives the plan back a row.

         `v-show`, not `v-if`, for which screen is up: tearing the plan down on every tab
         switch destroyed its <img>, so coming back repainted from a grey box and popped the
         pins in a frame later — indistinguishable from a reload, though the bytes were in
         cache the whole time. Hidden, the element and its decoded image survive and the switch
         is a repaint. `v-if` still guards whether there is a SITE at all, since the component
         cannot render without one.

         The cost, stated: the plan is now mounted whichever access screen is up, so a holder
         who lands on Portals downloads a plan they may not open. One image, and it is exactly
         the one they need if they do tap Plan — cheaper than the machinery to defer it. -->
    <BadgeFloorplan
      v-if="activeSite"
      v-show="view === 'plan'"
      :key="activeSite.id"
      :location="activeSite"
      :enabled="canAct"
      :disabled-note="readonly ? 'Read-only preview — open doors from Live View' : undefined"
      class="flex-1 min-h-0"
    >
      <template v-if="plans.length > 1" #header>
        <select
          class="select select-bordered select-sm min-h-11 w-full text-sm"
          aria-label="Site"
          :value="activeSite.id"
          @change="setSite(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
      </template>
    </BadgeFloorplan>

    <!-- Portals.
         These lists used to bound themselves to ~256px and scroll inside the page, because all
         of them were stacked and a badge with twenty remote doors turned the tab into a
         document. With one view on screen at a time that reason is gone, and what is left is a
         nested scroll region on a phone — so they grow and the page's own scroll carries them. -->
    <div v-if="view === 'portals'" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="card-title text-base">Portals</h2>
          <span class="text-xs text-base-content/50">{{ remotePortals.length }}</span>
        </div>

        <BadgeFilterInput
          v-if="showPortalFilter"
          v-model="portalQuery"
          placeholder="Filter by name or building"
          label="Filter your portals"
        />

        <p v-if="!filteredPortals.length" class="text-sm text-base-content/50 py-4 text-center">
          Nothing matches “{{ portalQuery }}”.
        </p>

        <div class="space-y-2">
          <div v-for="p in filteredPortals" :key="p.id" class="space-y-1">
            <button
              class="btn w-full justify-between"
              :class="toneClass(p.id, 'btn-primary')"
              :disabled="busy[p.id] || !canAct"
              @click="unlock(p)"
            >
              <span class="text-left">
                {{ p.name }}
                <span v-if="p.location" class="block text-xs font-normal opacity-70">{{ p.location }}</span>
              </span>
              <span v-if="busy[p.id]" class="loading loading-spinner loading-sm"></span>
            </button>
            <BadgeActionNote :result="results[p.id]" class="px-1" />
          </div>
        </div>
      </div>
    </div>

    <!-- Areas. Arm and disarm are separate rights, so they are separate buttons and
         either may be absent. -->
    <div v-if="view === 'areas'" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="card-title text-base">Areas</h2>
          <span class="text-xs text-base-content/50">{{ remoteAreas.length }}</span>
        </div>
        <div class="space-y-3">
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
            <!-- No tint on the area buttons, unlike portals and controls: an area has TWO of
                 them sharing one outcome, and the chip above already shows the resulting state
                 once the re-fetch lands. Colouring both Arm and Disarm green would say the
                 wrong thing about whichever one was not pressed. -->
            <BadgeActionNote :result="results[a.id]" class="px-1" />
          </div>
        </div>
      </div>
    </div>

    <!-- Aux outputs: one momentary action each. -->
    <div v-if="view === 'controls'" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <div class="flex items-baseline justify-between gap-2">
          <h2 class="card-title text-base">Controls</h2>
          <span class="text-xs text-base-content/50">{{ remoteOutputs.length }}</span>
        </div>
        <div class="space-y-2">
          <div v-for="o in remoteOutputs" :key="o.id" class="space-y-1">
            <!-- Idle tone is '' so `btn-outline` stays and the tone composes with it: a control
                 flashing a green OUTLINE is the same signal as a door flashing solid, without a
                 momentary-relay button impersonating the primary action on the screen. -->
            <button
              class="btn btn-outline w-full justify-between"
              :class="toneClass(o.id, '')"
              :disabled="busy[o.id] || !canAct"
              @click="pulse(o)"
            >
              <span class="text-left">
                {{ o.name }}
                <span v-if="o.location" class="block text-xs font-normal opacity-70">{{ o.location }}</span>
              </span>
              <span v-if="busy[o.id]" class="loading loading-spinner loading-sm"></span>
            </button>
            <BadgeActionNote :result="results[o.id]" class="px-1" />
          </div>
        </div>
      </div>
    </div>

    <!-- Everything on the badge that can only be used in person: grouped by building,
         collapsible, filterable, and bounded. See BadgeOnSiteList. -->
    <BadgeOnSiteList v-if="view === 'onsite'" :items="onSiteItems" />

    <div v-if="nothingGranted" class="text-center text-sm text-base-content/50 py-8">
      {{ readonly ? 'Nothing is assigned to this badge yet.' : 'Nothing is assigned to your badge yet.' }}
    </div>
  </div>
</template>
