<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { badgePb } from '@/utils/badgePb'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import ThemeToggle from '@/components/common/ThemeToggle.vue'
import BadgePassPanel from './BadgePassPanel.vue'
import BadgeAccessPanel from './BadgeAccessPanel.vue'
import BadgePasswordModal from './BadgePasswordModal.vue'
import type { BadgeMe } from '@/types/badge'

/**
 * The badge shell: one fetch of GET /api/badge/me, two tabs over it.
 *
 *   Badge   the face — photo, QR, validity (BadgePassPanel)
 *   Access  what it can do — doors, areas, controls (BadgeAccessPanel)
 *
 * Split because the two halves answer different questions and are reached at different
 * moments: the face is what you hold up at a desk, the Access tab is what you use with
 * your hands full outside a door. Keeping the QR on its own tab also means the thing
 * most worth photographing is not on screen while someone is pressing buttons.
 *
 * The tab is a query param so a holder can bookmark the half they actually use, and so
 * "open my doors" can be linked to directly.
 *
 * # Why this is a fixed-height shell and not a document
 *
 * `h-dvh` + `overflow-hidden` on the frame, with ONE scroll region inside it. A badge is
 * used one-handed, often outdoors, often in a hurry — the header (who you are, sign out)
 * and the tabs must not scroll away from under a thumb, and "swipe up to find the unlock
 * button" is the wrong interaction for a door.
 *
 * `dvh` rather than `vh` on purpose: mobile Safari's `vh` is the tallest viewport, so a
 * `h-screen` frame is taller than the visible area whenever the URL bar is showing, which
 * puts the bottom of the content under it. `dvh` tracks the viewport as it actually is.
 *
 * Everything else follows from that frame: the password form became a modal (rare,
 * deliberate act — no reason for it to occupy the badge permanently), and the Access tab is
 * handed a DEFINITE height rather than growing to its content, which is what lets its floor
 * plan fill the screen exactly instead of being sized by the image it happens to load. A
 * list longer than the frame still overflows into the scroll region above; the difference is
 * only that a view can now know how much room it has.
 */
const route = useRoute()
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

type Tab = 'badge' | 'access'
function isTab(v: unknown): v is Tab {
  return v === 'badge' || v === 'access'
}

const me = ref<BadgeMe | null>(null)
const loading = ref(true)
const loadError = ref('')
const tab = ref<Tab>(isTab(route.query.tab) ? route.query.tab : 'badge')
const showPasswordModal = ref(false)
const passwordSaved = ref(false)
/** Set while a manual refresh is in flight, so the whole badge is not torn down for one. */
const refreshing = ref(false)

function setTab(t: Tab) {
  tab.value = t
  router.replace({ path: route.path, query: { ...route.query, tab: t } }).catch(() => {})
}

/** Shown on the Access tab so a holder can see at a glance whether there is anything there. */
const actionCount = computed(() => {
  const m = me.value
  if (!m) return 0
  return m.portals.length + m.areas.length + m.outputs.length
})

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

function openPasswordModal() {
  dismissMenu()
  passwordSaved.value = false
  showPasswordModal.value = true
}

function manualRefresh() {
  dismissMenu()
  refresh()
}

onMounted(load)
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
             one irreversible action on this screen a thumb-width from the tabs.
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
      <!-- Tabs stay pinned: they are how you get to the other half of the badge.
           `h-11` because DaisyUI's `.tab` is 2rem — fine in a dense desktop console, too
           short for the primary navigation of a one-handed phone screen. -->
      <div class="shrink-0 px-3">
        <div role="tablist" class="tabs tabs-boxed mx-auto max-w-sm">
          <button
            role="tab"
            class="tab h-11 flex-1"
            :class="tab === 'badge' ? 'tab-active' : ''"
            @click="setTab('badge')"
          >
            Badge
          </button>
          <button
            role="tab"
            class="tab h-11 flex-1 gap-1"
            :class="tab === 'access' ? 'tab-active' : ''"
            @click="setTab('access')"
          >
            Access
            <span v-if="actionCount" class="badge badge-sm">{{ actionCount }}</span>
          </button>
        </div>
      </div>

      <!-- The ONE scroll region. `min-h-0` is what lets it shrink inside the flex column
           instead of pushing the frame taller than the viewport. -->
      <main class="flex-1 min-h-0 overflow-y-auto overscroll-contain px-3 py-3">
        <!-- `h-full` + flex column so the Access tab's floor-plan view can fill the screen
             rather than being sized by its image. This is the definite height the whole chain
             below depends on — the panel takes it as a flex item, and the plan card's image
             caps itself against it. A list taller than this still overflows and this region
             still scrolls, exactly as before; what changed is only that a view CAN know how
             much room it has. -->
        <div class="max-w-sm mx-auto flex h-full flex-col">
          <p v-if="passwordSaved" class="alert alert-success shrink-0 py-2 text-sm mb-3">
            Your password has been set.
          </p>
          <BadgePassPanel v-if="tab === 'badge'" :me="me" />
          <BadgeAccessPanel v-else :me="me" class="min-h-0 flex-1" @refresh="refresh" />
        </div>
      </main>
    </template>

    <BadgePasswordModal v-model:open="showPasswordModal" @saved="passwordSaved = true" />
  </div>
</template>
