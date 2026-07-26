<script setup lang="ts">
import { computed, ref } from 'vue'
import AppSidebar, { type NavItem, type NavSection } from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import HelpPanel from '@/components/ui/HelpPanel.vue'
import OperatorBadgeModal from '@/components/common/OperatorBadgeModal.vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()

// The primary navigation. `capability` gates an item to operators who hold it;
// filtering happens here (in the layout that owns the graph) so AppSidebar stays
// a pure, app-agnostic renderer. Read is a universal floor, so ungated items
// show for any authenticated operator.
type NavItemCap = NavItem & { capability?: string }
interface NavSectionCap { title?: string; items: NavItemCap[] }

const sections: NavSectionCap[] = [
  {
    items: [
      { label: 'Overview', icon: '📊', path: '/' },
    ],
  },
  {
    title: 'Monitoring',
    items: [
      { label: 'Live View', icon: '🗺️', path: '/monitor' },
      { label: 'Alarm Console', icon: '🚨', path: '/alarms' },
      { label: 'Events', icon: '📋', path: '/events' },
      { label: 'Reports', icon: '📈', path: '/reports' },
    ],
  },
  {
    title: 'People & Access',
    items: [
      // Three peers, not a parent with two children. Cardholders and Visitors are the
      // same collection split by `kind`, and Credentials is what actually opens a door —
      // indenting either under Cardholders suggested a containment that does not exist.
      { label: 'Cardholders', icon: '🪪', path: '/cardholders' },
      { label: 'Visitors', icon: '👋', path: '/visitors' },
      { label: 'Credentials', icon: '🎫', path: '/credentials' },
      { label: 'Roles', icon: '🏷️', path: '/roles' },
      { label: 'Access Groups', icon: '🗝️', path: '/access-groups' },
    ],
  },
  {
    title: 'Facility',
    items: [
      { label: 'Locations', icon: '🏢', path: '/locations' },
      { label: 'Controllers', icon: '⚙️', path: '/controllers' },
      { label: 'Portals', icon: '🚪', path: '/portals' },
      { label: 'Aux Inputs', icon: '🔌', path: '/aux-inputs' },
      { label: 'Aux Outputs', icon: '🔆', path: '/aux-outputs' },
      { label: 'Areas', icon: '🛡️', path: '/areas' },
      { label: 'Schedules', icon: '🗓️', path: '/schedules' },
      { label: 'Holiday Calendars', icon: '📆', path: '/holiday-calendars' },
      { label: 'Holidays', icon: '📅', path: '/holidays', child: true },
    ],
  },
  {
    title: 'Administration',
    items: [
      { label: 'Import', icon: '📥', path: '/import', capability: 'enroll' },
      { label: 'Operators', icon: '👥', path: '/operators', capability: 'operators' },
      { label: 'Audit Log', icon: '📜', path: '/audit-log', capability: 'operators' },
    ],
  },
]

// Hide items the operator lacks the capability for; drop sections left empty.
const visibleSections = computed<NavSection[]>(() =>
  sections
    .map((s) => ({ ...s, items: s.items.filter((i) => !i.capability || authStore.can(i.capability)) }))
    .filter((s) => s.items.length > 0),
)

// Account-dropdown entries: tools belonging to the signed-in operator rather than to a
// part of the system. Scan to Unlock is a phone-in-hand utility for whoever is walking
// the building — it sat under Facility, where it read as a thing you configure. Door
// placards moved the other way, to a print action on the Portals list, since they are
// generated FROM portals. Gated on `command`, because scanning issues a door grant.
// "My badge" is ungated: reading one's own badge needs no capability, and whether there
// IS one is decided by the server (a cardholder with this operator in its `operator`
// field). Shown to everyone rather than probed for at load — the modal explains the
// unlinked case, which costs nothing until someone opens it.
const accountItems = computed<NavItemCap[]>(() =>
  (
    [
      { label: 'My badge', icon: '🪪', path: '', action: 'my-badge' },
      { label: 'Scan to Unlock', icon: '📷', path: '/portals/scan', capability: 'command' },
    ] as NavItemCap[]
  ).filter((i) => !i.capability || authStore.can(i.capability)),
)

// The badge modal, opened from the account dropdown. Lives here rather than in the
// sidebar because the sidebar's job is navigation, not owning a dialog.
const badgeOpen = ref(false)
function onAccountAction(name: string) {
  if (name === 'my-badge') badgeOpen.value = true
}
</script>

<template>
  <div class="drawer lg:drawer-open h-dvh">
    <input id="sidebar-drawer" type="checkbox" class="drawer-toggle" />

    <!-- Main content -->
    <div class="drawer-content flex flex-col min-h-0">
      <AppHeader />
      <main class="flex-1 min-h-0 overflow-y-auto overscroll-y-contain bg-base-200">
        <div class="mx-auto w-full max-w-7xl p-4 lg:p-6 pad-safe-bottom">
          <router-view />
        </div>
      </main>
    </div>

    <!-- Sidebar -->
    <div class="drawer-side z-40">
      <label for="sidebar-drawer" class="drawer-overlay"></label>
      <AppSidebar :sections="visibleSections" :account-items="accountItems" @action="onAccountAction" />
    </div>

    <!-- Contextual help: slide-over panel (opened from the header icon on mobile, the inline button on desktop) -->
    <HelpPanel />

    <!-- This operator's own badge, if a cardholder record points at their account. -->
    <OperatorBadgeModal v-model:open="badgeOpen" />
  </div>
</template>
