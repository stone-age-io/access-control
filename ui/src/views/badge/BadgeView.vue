<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { badgePb } from '@/utils/badgePb'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import ThemeToggle from '@/components/common/ThemeToggle.vue'
import BadgePassPanel from './BadgePassPanel.vue'
import BadgeAccessPanel from './BadgeAccessPanel.vue'
import BadgePasswordModal from './BadgePasswordModal.vue'
import BadgeNavBar from './BadgeNavBar.vue'
import {
  BADGE_FACE,
  badgeViews,
  type BadgeNavItem,
  type BadgeTabKey,
  type BadgeViewKey,
} from './badgeNav'
import type { BadgeLive, BadgeMe } from '@/types/badge'

/**
 * The badge shell: one fetch of GET /api/badge/me, one navigation bar over it.
 *
 * # Why this is a fixed-height shell and not a document
 *
 * `h-dvh` + `overflow-hidden` on the frame, with ONE scroll region inside it. A badge is
 * used one-handed, often outdoors, often in a hurry — the header (who you are, sign out)
 * and the navigation must not scroll away from under a thumb, and "swipe up to find the
 * unlock button" is the wrong interaction for a door.
 *
 * `dvh` rather than `vh` on purpose: mobile Safari's `vh` is the tallest viewport, so a
 * `h-screen` frame is taller than the visible area whenever the URL bar is showing, which
 * puts the bottom of the content under it. `dvh` tracks the viewport as it actually is.
 *
 * # Why the navigation is ONE flat bar at the bottom
 *
 * It used to be two levels: Badge/Access tabs pinned under the header, and inside the Access
 * tab a second row of segment pills for plan / portals / areas / controls / on-site. That is
 * three rows of chrome above the content on a phone, and the pills WRAPPED to a second line
 * as soon as a holder had four segments — so the badge with the most on it got the least room
 * to show it.
 *
 * Flattening them solves the wrap by construction rather than by squeezing (see BadgeNavBar for
 * why equal columns can't), moves navigation into the thumb zone at the bottom of the screen,
 * and hands back the ~90px the two rows cost at the top — which is exactly the space the floor
 * plan wanted.
 *
 * The flattening is honest about the hierarchy too: from the holder's side these were never
 * two levels. "My photo and QR" and "my doors" are peers — different screens of one badge.
 *
 * # What the shell owns, and why
 *
 * The bar has to know which access views exist, so the /api/badge/live fetch and the view
 * derivation (`badgeNav.ts`) live up here and BadgeAccessPanel became a pure renderer, told
 * which view to draw. The operator's preview modal drives the same bar over the same derivation
 * and the same panel, so what an operator troubleshoots with cannot disagree with what the
 * holder sees; this shell's remaining job is the frame, the fetches and the selection.
 *
 * The Access panel is also handed a DEFINITE height rather than growing to its content, which
 * is what lets its floor plan fill the screen exactly instead of being sized by the image it
 * happens to load. A view taller than the frame still overflows into the scroll region; the
 * difference is only that a view CAN know how much room it has.
 */
const route = useRoute()
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

const me = ref<BadgeMe | null>(null)
const plans = ref<BadgeLive['locations']>([])
const loading = ref(true)
const loadError = ref('')
const showPasswordModal = ref(false)
const passwordSaved = ref(false)
/** Set while a manual refresh is in flight, so the whole badge is not torn down for one. */
const refreshing = ref(false)

// --- which screen is showing -------------------------------------------------
//
// One selection over the flattened list, remembered two ways because they answer different
// questions: the query param so a holder can bookmark (and we can link to) the screen they
// actually use, and localStorage so simply opening the badge lands where they left it.
const STORAGE_KEY = 'sa.badge.view'

/** The face first, then whatever this badge has to show. Always at least two items. */
const navItems = computed<BadgeNavItem[]>(() => [
  BADGE_FACE,
  ...(me.value ? badgeViews(me.value, plans.value) : []),
])

function initialTab(): string {
  const q = route.query.tab
  if (typeof q === 'string' && q) return q
  return localStorage.getItem(STORAGE_KEY) || ''
}
const chosen = ref<string>(initialTab())

/**
 * The screen actually rendered. Resolved against the live list rather than stored, so the two
 * ways a choice can go stale need no cleanup: a segment that disappears — a location that
 * opted out of plans, a pass whose grants changed — stops matching and the face takes over.
 * It is also what makes the Plan segment take focus the moment /api/badge/live lands, having
 * started on whatever was available a tick earlier.
 *
 * `?tab=access` is honoured as the first access segment: the two-level version's URL is
 * documented as bookmarkable, and one flattening should not break a link someone saved.
 */
const activeTab = computed<BadgeTabKey>(() => {
  const items = navItems.value
  const want = chosen.value === 'access' ? items[1]?.key : chosen.value
  return items.find((i) => i.key === want)?.key ?? items[0].key
})

/** Null on the face, which is the one screen the Access panel does not draw. */
const accessView = computed<BadgeViewKey | null>(() =>
  activeTab.value === 'badge' ? null : activeTab.value,
)

function setTab(key: BadgeTabKey) {
  chosen.value = key
  localStorage.setItem(STORAGE_KEY, key)
  router.replace({ path: route.path, query: { ...route.query, tab: key } }).catch(() => {})
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    me.value = await badgePb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
  } catch (err: any) {
    if (err?.status === 401) {
      badgeAuth.logout()
      router.push({ name: 'Login', query: { as: 'badge' } })
      return
    }
    loadError.value = 'Could not load your badge. Try again in a moment.'
  } finally {
    loading.value = false
  }
}

/**
 * Floor plans are a best-effort request: the lists are the complete surface, and a plan is an
 * upgrade a location opts into (`locations.badge_floorplan`). Most installs return an empty
 * list, so a failure here is silent — there is nothing to tell the holder about a picture
 * they were never promised.
 */
async function loadPlans() {
  try {
    const res = await badgePb.send<BadgeLive>('/api/badge/live', { method: 'GET' })
    plans.value = res.locations || []
  } catch {
    plans.value = []
  }
}

/**
 * Re-fetch after an action that changes server-held state (arming), and on the explicit
 * Refresh in the menu. Deliberately a full reload of the badge rather than a local patch:
 * an area's arm-state is resolved server-side from the policy graph, so asking again is the
 * only honest way to show it.
 *
 * Refresh is offered manually rather than polled: the one piece of state a holder can act
 * on is an area's arm-state, and a pass that has just been extended or revoked is the other
 * thing worth re-asking about — neither is worth a timer on a phone battery, and both are
 * moments the holder knows about.
 */
async function refresh() {
  refreshing.value = true
  try {
    me.value = await badgePb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
    await loadPlans()
  } catch {
    // Leave the previous view in place: the action itself already reported its outcome,
    // and replacing that with a load error would be misleading.
  } finally {
    refreshing.value = false
  }
}

async function signOut() {
  badgeAuth.logout()
  router.push({ name: 'Login', query: { as: 'badge' } })
}

/**
 * Close the open dropdown after choosing something. DaisyUI's CSS-only dropdown stays open
 * until focus leaves, which on a phone means it hangs over the modal it just opened.
 */
function dismissMenu() {
  if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
}

/**
 * "Your password has been set" is a confirmation, not a status, so it expires — the same rule
 * `useBadgeAction` applies to an unlock. It used to sit above the badge until the next reload,
 * which by the following morning reads as a state ("my password is set") rather than as an
 * answer to something the holder did a moment ago.
 */
const SAVED_NOTE_MS = 5000
let savedNoteTimer: ReturnType<typeof setTimeout> | undefined

function clearSavedNote() {
  if (savedNoteTimer !== undefined) {
    clearTimeout(savedNoteTimer)
    savedNoteTimer = undefined
  }
  passwordSaved.value = false
}

function notePasswordSaved() {
  clearSavedNote()
  passwordSaved.value = true
  savedNoteTimer = setTimeout(clearSavedNote, SAVED_NOTE_MS)
}

onUnmounted(clearSavedNote)

function openPasswordModal() {
  dismissMenu()
  clearSavedNote()
  showPasswordModal.value = true
}

function manualRefresh() {
  dismissMenu()
  refresh()
}

onMounted(() => {
  load()
  loadPlans()
})
</script>

<template>
  <!-- The frame. Fixed to the viewport and never scrolls; exactly one region inside it does. -->
  <div class="h-dvh flex flex-col overflow-hidden bg-base-200">
    <!-- Header -->
    <header class="shrink-0 flex items-center justify-between gap-2 px-3 py-2 pad-safe-top">
      <div class="flex items-center gap-2 min-w-0">
        <BrandLogo :size="26" />
        <span class="text-sm font-medium truncate">{{ branding.appName }}</span>
        <!-- In the header, not in the menu: choosing Refresh closes the menu, so a spinner
             inside it would be dismissed at the moment it became relevant. -->
        <span v-if="refreshing" class="loading loading-spinner loading-xs shrink-0"></span>
      </div>

      <div class="flex items-center shrink-0">
        <ThemeToggle />

        <!-- Account menu. Sign out used to be a bare button in the header, which put the
             one irreversible action on this screen a thumb-width from the navigation.
             `btn-square` with no size modifier: 48px, see ThemeToggle for why `btn-sm`
             is the wrong tool here. -->
        <div class="dropdown dropdown-end">
          <button
            tabindex="0"
            class="btn btn-ghost btn-square"
            aria-label="Account menu"
            :title="badgeAuth.email || 'Account'"
          >
            <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
            </svg>
          </button>
          <!-- Not `menu-sm`: it pads rows to .25rem, giving ~28px tall targets stacked a
               few pixels apart — and one of them signs you out. `min-h-11` puts every row
               at the 44px floor, which the default menu padding alone does not reach. -->
          <ul tabindex="0" class="dropdown-content menu bg-base-100 rounded-box z-50 w-64 p-2 shadow-lg">
            <li class="menu-title truncate">{{ badgeAuth.email || 'Signed in' }}</li>
            <li><button type="button" class="min-h-11" @click="manualRefresh">Refresh</button></li>
            <li>
              <button type="button" class="min-h-11" @click="openPasswordModal">
                {{ badgeAuth.hasPassword ? 'Change password' : 'Set a password' }}
              </button>
            </li>
            <li>
              <button type="button" class="min-h-11 text-error" @click="signOut">Sign out</button>
            </li>
          </ul>
        </div>
      </div>
    </header>

    <div v-if="loading" class="flex-1 flex items-center justify-center">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div v-else-if="loadError" class="flex-1 flex items-center justify-center p-4">
      <div class="alert alert-error text-sm max-w-sm">
        <span>{{ loadError }}</span>
        <button class="btn btn-xs" @click="load">Retry</button>
      </div>
    </div>

    <template v-else-if="me">
      <!-- The ONE scroll region. `min-h-0` is what lets it shrink inside the flex column
           instead of pushing the frame taller than the viewport. -->
      <main class="flex-1 min-h-0 overflow-y-auto overscroll-contain px-3 py-3">
        <!-- `h-full` + flex column so the Access panel's floor-plan view can fill the screen
             rather than being sized by its image. This is the definite height the whole chain
             below depends on. -->
        <div class="max-w-sm mx-auto flex h-full flex-col">
          <p
            v-if="passwordSaved"
            role="status"
            aria-live="polite"
            class="alert alert-success shrink-0 py-2 text-sm mb-3"
          >
            Your password has been set.
          </p>
          <!-- Kept alive, not rebuilt on every screen change. These two swap constantly, and
               tearing them down meant a fresh <img> each time: the floor plan repainted from a
               grey box and the photo re-decoded, which reads as a reload even when nothing is
               fetched. Alive, the elements and their decoded images survive, so a screen change
               is a repaint — and the plan keeps its measured box and selected marker, and a
               list keeps its filter text, across a trip to the face and back.

               This is only half of it: the photo URL also had to stop changing per mount, or
               the very first paint of the face would still be a download. See
               composables/useFileUrl.

               Nothing may sit BETWEEN the two branches below, not even a comment: the compiler
               stashes a comment there into the v-else branch in dev only, which makes that
               branch a Fragment, and KeepAlive silently skips a child that is not a stateful
               component. The face would then be cached in production and not in dev — so the
               note that belongs on BadgePassPanel is here instead: `my-auto` centres the face
               in whatever height is left over and collapses to nothing when there is none, so
               a short screen scrolls rather than clipping the top of the photo, which is what
               `justify-center` on the column would have done. -->
          <KeepAlive>
            <BadgeAccessPanel
              v-if="accessView"
              :me="me"
              :plans="plans"
              :view="accessView"
              class="min-h-0 flex-1"
              @refresh="refresh"
            />
            <BadgePassPanel v-else :me="me" class="my-auto" />
          </KeepAlive>
        </div>
      </main>

      <!-- Pinned to the bottom of the frame: it is how you reach every other screen of the
           badge, and the bottom of a phone is where a thumb rests. `pad-safe-bottom` keeps the
           row off the home indicator — that is this SHELL's concern, not the bar's, which is why
           it is here and not in the component (the operator's preview renders the same bar
           inside a dialog, where a safe-area inset would just be dead padding). -->
      <BadgeNavBar
        :items="navItems"
        :active="activeTab"
        class="shrink-0 pad-safe-bottom"
        @select="setTab"
      />
    </template>

    <BadgePasswordModal v-model:open="showPasswordModal" @saved="notePasswordSaved" />
  </div>
</template>
