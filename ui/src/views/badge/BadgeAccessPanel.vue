<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { badgePb } from '@/utils/badgePb'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import BadgeFloorplan from './BadgeFloorplan.vue'
import BadgeOnSiteList, { type OnSiteItem } from './BadgeOnSiteList.vue'
import BadgeFilterInput from './BadgeFilterInput.vue'
import { actionErrorText } from './reasonText'
import type {
  BadgeArea,
  BadgeLive,
  BadgeLiveLocation,
  BadgeMe,
  BadgeOutput,
  BadgePortal,
} from '@/types/badge'

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
// A best-effort request when the caller supplied none: the lists are the complete surface,
// and a plan is an upgrade a location opts into (`locations.badge_floorplan`). Most
// installs return an empty list, so a failure here is silent — there is nothing to tell the
// holder about a picture they were never promised.
const fetchedPlans = ref<BadgeLive['locations']>([])
const plans = computed<BadgeLive['locations']>(() => props.plans ?? fetchedPlans.value)

onMounted(async () => {
  if (props.plans !== undefined) return // the caller already has them
  try {
    const res = await badgePb.send<BadgeLive>('/api/badge/live', { method: 'GET' })
    fetchedPlans.value = res.locations || []
  } catch {
    fetchedPlans.value = []
  }
})

// --- which view is showing --------------------------------------------------
//
// The operator's Live View switches between a floor plan, portals, areas, and aux I/O. This
// is the same idea sized for a badge, with one rule that makes the difference: a segment
// exists only if the holder has something in it, and with nothing to choose between the
// switcher does not render at all. A site has hundreds of points and an operator is hunting;
// a holder typically has a handful of doors and no areas or controls at all, so fixed
// segments would be mostly-empty chrome over a three-item list.
//
// "On site" sits beside the three kinds rather than under them because the split a holder
// cares about is what they can press a button for versus what they must walk to — and past
// a few doors that list wants grouping by building, which BadgeOnSiteList does and the
// action lists do not.
//
// The portal segment is labelled "Portals", not "Doors", because that is what the rest of
// the system calls them and because a portal is not always a door — a turnstile, a gate, an
// elevator and a logical portal all show up here, and a holder whose badge opens the vehicle
// gate should not have to read it as a door.
type ViewKey = 'plan' | 'portals' | 'areas' | 'controls' | 'onsite'
interface ViewTab {
  key: ViewKey
  label: string
  icon: string
  /**
   * Shown beside the label. For a list segment it is how many rows are in it. For the plan
   * it is how many SITES there are, and it is omitted at one — the plan shows one picture at
   * a time, so "Plan 1" would be counting the thing already on screen.
   */
  count?: number
}

const VIEW_STORAGE_KEY = 'sa.badge.view'

const views = computed<ViewTab[]>(() => {
  const out: ViewTab[] = []
  // The count here is a count of SITES, not of pins: it is the only thing on the tab row
  // that says a second plan exists at all. Without it a two-site holder saw one plan and
  // nothing hinting at the other.
  if (plans.value.length)
    out.push({
      key: 'plan',
      label: 'Plan',
      icon: '🗺️',
      count: plans.value.length > 1 ? plans.value.length : undefined,
    })
  if (remotePortals.value.length)
    out.push({ key: 'portals', label: 'Portals', icon: '🚪', count: remotePortals.value.length })
  if (remoteAreas.value.length)
    out.push({ key: 'areas', label: 'Areas', icon: '🛡️', count: remoteAreas.value.length })
  if (remoteOutputs.value.length)
    out.push({ key: 'controls', label: 'Controls', icon: '⚡', count: remoteOutputs.value.length })
  if (onSiteItems.value.length)
    out.push({ key: 'onsite', label: 'On site', icon: '📍', count: onSiteItems.value.length })
  return out
})

function storedView(): ViewKey | '' {
  // Never in the operator's preview: it renders this component to show what the HOLDER
  // sees, so a segment chosen while troubleshooting must not follow the operator to their
  // own badge.
  //
  // A holder who last chose the old 'doors' key falls through to the first segment, which
  // is what this returning '' already means and needs no migration: one visit re-picks it.
  if (props.readonly) return ''
  const raw = localStorage.getItem(VIEW_STORAGE_KEY)
  return raw === 'plan' || raw === 'portals' || raw === 'areas' || raw === 'controls' || raw === 'onsite'
    ? raw
    : ''
}
const chosen = ref<ViewKey | ''>(storedView())

/**
 * The view actually rendered. Resolved rather than stored, so the two ways a choice can go
 * stale are handled in one place: nothing chosen yet, and a choice whose segment is gone —
 * a location that opted out of plans, or a pass whose grants changed. Falling through to the
 * first segment also means the plan takes over once `/api/badge/live` lands, which is the
 * right default: "which door am I at" is the question a plan answers and a list does not.
 */
const activeView = computed<ViewKey | ''>(() => {
  const available = views.value
  if (chosen.value && available.some((v) => v.key === chosen.value)) return chosen.value
  return available[0]?.key ?? ''
})

function setView(key: ViewKey) {
  chosen.value = key
  if (!props.readonly) localStorage.setItem(VIEW_STORAGE_KEY, key)
}

// --- which site's plan is showing -------------------------------------------
//
// /api/badge/live returns every site the holder has something placed at, and this used to
// render all of them stacked. That is not wrong so much as invisible: each plan is a
// full-width image, so on a phone the first one plus its action bar is about a screen, and
// the second card's header starts below the fold with nothing to suggest it is there.
//
// So the plan segment shows ONE site, chosen from a row of its own. This is the same
// adaptive rule as the view switcher above it — with one site the row does not render at
// all, which is the ordinary badge — and the same shape the operator console reaches for,
// where /monitor is an all-sites overview and /monitor/:locationId is one building's plan.
//
// Rendering one at a time also settles something the stacked version got wrong for free:
// each BadgeFloorplan owns its own selection, so two plans on screen could each caption a
// selected door, and the one scrolled off the top kept its stale result text. Keying the
// component by site id means switching destroys and rebuilds it, so a selection can never
// outlive the plan it was made on.
const SITE_STORAGE_KEY = 'sa.badge.site'

function storedSite(): string {
  // Not in the operator's preview, for the same reason the view choice is not: this renders
  // what the HOLDER sees, and a site picked while troubleshooting must not follow the
  // operator to their own badge.
  if (props.readonly) return ''
  return localStorage.getItem(SITE_STORAGE_KEY) || ''
}
const chosenSite = ref<string>(storedSite())

/**
 * The site actually rendered. Resolved rather than stored, like `activeView`, so a stale
 * choice needs no cleanup anywhere: a site that opted out of badge plans, or one whose last
 * granted door was removed, simply stops matching and the first site takes over. Remembering
 * is never worse than not — the fallback is the same name-sorted first entry the server
 * already returns.
 */
const activeSite = computed<BadgeLiveLocation | undefined>(() => {
  const available = plans.value
  return available.find((l) => l.id === chosenSite.value) ?? available[0]
})

function setSite(id: string) {
  chosenSite.value = id
  if (!props.readonly) localStorage.setItem(SITE_STORAGE_KEY, id)
}

// --- filtering the portal list ----------------------------------------------
//
// The same shape BadgeOnSiteList already uses, and offered on the same terms: past a
// handful of doors you know the name of the one you want, and below that a search box is
// chrome over a list you can read at a glance. Matched against the building name too,
// because "warehouse" is how a holder with four sites narrows to one — which is also why
// this stayed a filter rather than becoming a location picker: a picker set to one building
// would keep the other three's doors off the screen for as long as it stayed set, and this
// is the surface where a hidden door is a holder standing outside one.
//
// Deliberately NOT persisted, unlike the segment and the plan's site. A filter is a thing
// you are doing right now; coming back to a badge that shows three of your twelve doors,
// because of something typed last week, is how a holder concludes their access was revoked.
const PORTAL_FILTER_THRESHOLD = 6

const portalQuery = ref('')
const showPortalFilter = computed(() => remotePortals.value.length >= PORTAL_FILTER_THRESHOLD)

const filteredPortals = computed<BadgePortal[]>(() => {
  const q = portalQuery.value.trim().toLowerCase()
  if (!q) return remotePortals.value
  return remotePortals.value.filter((p) =>
    `${p.name} ${p.location}`.toLowerCase().includes(q),
  )
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
  <!-- A flex column, not a stack of margins, because the plan view has to fill the height it
       is given: the shell hands this panel a definite height (see BadgeView's ONE scroll
       region), the rows below are all `shrink-0`, and the plan card takes the rest. The list
       views leave the column's slack empty and overflow it when they are long, which the
       shell's scroll region carries exactly as before. -->
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

    <!-- The switcher. Wrapping buttons rather than DaisyUI `tabs-boxed`, which clips labels
         and double-scrolls once several segments overflow a phone (the same reason Reports
         does it this way); `min-h-11` because this is badge chrome a thumb aims at. Hidden
         entirely at one segment — a switcher with nothing to switch to is decoration. -->
    <div v-if="views.length > 1" role="tablist" class="flex shrink-0 flex-wrap gap-2">
      <button
        v-for="v in views"
        :key="v.key"
        type="button"
        role="tab"
        :aria-selected="activeView === v.key"
        class="inline-flex min-h-11 items-center gap-1.5 rounded-lg px-3 text-sm font-medium whitespace-nowrap transition-colors"
        :class="
          activeView === v.key
            ? 'bg-primary text-primary-content shadow-sm'
            : 'bg-base-200 text-base-content/70 hover:bg-base-300'
        "
        @click="setView(v.key)"
      >
        <span aria-hidden="true">{{ v.icon }}</span>
        <span>{{ v.label }}</span>
        <span v-if="v.count" class="opacity-60">{{ v.count }}</span>
      </button>
    </div>

    <!-- Floor plans, when a location opted in. First segment, because "which door am I at"
         is the question a plan answers better than a list ever does. -->
    <template v-if="activeView === 'plan'">
      <!-- One site at a time, picked from a native select. A row of pills was the first
           attempt and it competes with the segment row directly above it — two rows of
           chips, same size, different meaning. A select is one line whatever the site
           count, hands the choosing to the platform's own picker (a wheel on iOS, a sheet
           on Android), and above all gives the plan back the vertical space: the whole
           point of this view is the picture. `min-h-11` since DaisyUI's `select-sm` is
           2rem and this is badge chrome a thumb aims at. Absent at one site. -->
      <select
        v-if="plans.length > 1"
        class="select select-bordered select-sm min-h-11 w-full shrink-0 text-sm"
        aria-label="Site"
        :value="activeSite?.id ?? ''"
        @change="setSite(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>

      <!-- `flex-1 min-h-0` is the whole viewport-filling story on this side: the panel is a
           flex column of known height (see the root), so the plan card takes what the
           switcher and picker leave and the image sizes itself to that. -->
      <BadgeFloorplan
        v-if="activeSite"
        :key="activeSite.id"
        :location="activeSite"
        :enabled="canAct"
        :disabled-note="readonly ? 'Read-only preview — open doors from Live View' : undefined"
        class="flex-1 min-h-0"
      />
    </template>

    <!-- Portals.
         These three lists used to bound themselves to ~256px and scroll inside the page,
         because all of them were stacked and a badge with twenty remote doors turned the
         tab into a document. With one view on screen at a time that reason is gone, and
         what is left is a nested scroll region on a phone — so they grow and the page's own
         scroll carries them. -->
    <div v-if="activeView === 'portals'" class="card bg-base-100 shadow-sm">
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
    <div v-if="activeView === 'areas'" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <h2 class="card-title text-base">Areas</h2>
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
    <div v-if="activeView === 'controls'" class="card bg-base-100 shadow-sm">
      <div class="card-body gap-3 p-4">
        <h2 class="card-title text-base">Controls</h2>
        <div class="space-y-2">
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
    <BadgeOnSiteList v-if="activeView === 'onsite'" :items="onSiteItems" />

    <div v-if="nothingGranted" class="text-center text-sm text-base-content/50 py-8">
      {{ readonly ? 'Nothing is assigned to this badge yet.' : 'Nothing is assigned to your badge yet.' }}
    </div>
  </div>
</template>
