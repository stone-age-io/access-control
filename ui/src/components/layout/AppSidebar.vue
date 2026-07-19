<script setup lang="ts">
// Primary navigation sidebar. Drawer-side content: an overlay drawer below lg,
// a permanent column (with an icons-only compact rail) on lg+ — see MainLayout.
// Nav content arrives as `sections` from the layout so this stays a pure,
// app-agnostic renderer; the brand/logo come from the operator branding overlay.
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { useAuthStore } from '@/stores/auth'
import { useUIStore } from '@/stores/ui'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import { presetLabel } from '@/utils/capabilities'

export interface NavItem { label: string; icon: string; path: string; child?: boolean }
export interface NavSection { title?: string; items: NavItem[] }

const props = withDefaults(
  defineProps<{ sections: NavSection[]; brand?: string; home?: string }>(),
  { home: '/' },
)

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const uiStore = useUIStore()
const brandingStore = useBrandingStore()

const isLargeScreen = useMediaQuery('(min-width: 1024px)')
const effectiveCompact = computed(() => uiStore.sidebarCompact && isLargeScreen.value)

// Brand text: an explicit prop wins, else the operator branding overlay's app name.
const brandText = computed(() => props.brand ?? brandingStore.appName)

// Drop any sections left empty (the layout filters items by capability).
const visibleSections = computed<NavSection[]>(() =>
  props.sections.filter((s) => s.items.length > 0),
)

// Profile label: the matching preset name (e.g. "Door Ops"), else "Custom".
const roleLabel = computed(() => presetLabel(authStore.permissions))

// Longest matching prefix wins, so /cardholders/new highlights "Cardholders"
// and not also a shorter sibling. '/' only matches exactly.
const activePath = computed(() => {
  let best = ''
  for (const s of visibleSections.value)
    for (const i of s.items) {
      const match = i.path === '/' ? route.path === '/' : route.path === i.path || route.path.startsWith(i.path + '/')
      if (match && i.path.length > best.length) best = i.path
    }
  return best
})

function closeDrawer() {
  const drawer = document.getElementById('sidebar-drawer') as HTMLInputElement | null
  if (drawer) drawer.checked = false
}

// daisyUI dropdowns close on blur; a menu click keeps focus, so drop it.
function closeDropdown() {
  ;(document.activeElement as HTMLElement | null)?.blur()
}

async function handleLogout() {
  closeDropdown()
  await authStore.logout()
  closeDrawer()
  router.push('/login')
}
</script>

<template>
  <!-- h-full (= the .drawer-side's 100dvh, not min-h-full) caps the sidebar at the
       viewport so it never scrolls the whole column; the nav below is the only
       scroller (flex-1 min-h-0), keeping the brand header and account footer pinned. -->
  <aside
    class="bg-base-100 h-full flex flex-col border-r border-base-300 transition-all duration-300 ease-in-out z-20 pad-safe-top"
    :class="effectiveCompact ? 'w-20 min-w-[5rem]' : 'w-72 min-w-[18rem]'"
  >
    <!-- TOP: brand + collapse toggle -->
    <div class="flex-none p-3 pb-0">
      <div
        class="flex transition-all duration-300"
        :class="effectiveCompact ? 'flex-col items-center gap-3 py-2' : 'flex-row items-center justify-between px-2 py-2'"
      >
        <router-link :to="home" class="flex items-center gap-3 hover:opacity-80 transition-opacity overflow-hidden" @click="closeDrawer">
          <div class="w-10 h-10 flex items-center justify-center flex-shrink-0 text-primary">
            <BrandLogo :size="36" />
          </div>
          <span v-show="!effectiveCompact" class="font-bold text-lg tracking-tight whitespace-nowrap overflow-hidden">
            {{ brandText }}
          </span>
        </router-link>

        <button
          v-if="isLargeScreen"
          @click="uiStore.toggleCompact"
          class="btn btn-ghost btn-sm btn-square opacity-60 hover:opacity-100 transition-opacity"
          :title="uiStore.sidebarCompact ? 'Expand sidebar' : 'Collapse sidebar'"
          :aria-label="uiStore.sidebarCompact ? 'Expand sidebar' : 'Collapse sidebar'"
        >
          <span v-if="uiStore.sidebarCompact">»</span>
          <span v-else>«</span>
        </button>
      </div>
      <div class="divider my-0"></div>
    </div>

    <!-- NAVIGATION (the only scroller; min-h-0 lets this flex child shrink and scroll) -->
    <nav class="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-3 pb-2">
      <ul class="menu p-0 gap-1 w-full">
        <template v-for="(section, si) in visibleSections" :key="si">
          <li v-if="section.title && !effectiveCompact" class="menu-title px-2 pt-3 pb-1 text-[10px] uppercase tracking-widest opacity-50">
            {{ section.title }}
          </li>
          <li v-else-if="section.title && effectiveCompact" class="py-1">
            <div class="divider my-0"></div>
          </li>

          <li v-for="item in section.items" :key="item.path" :class="{ 'ml-4': item.child && !effectiveCompact }">
            <router-link
              :to="item.path"
              :class="{ active: item.path === activePath }"
              class="group relative"
              @click="closeDrawer"
            >
              <div
                v-if="effectiveCompact"
                class="tooltip tooltip-right absolute left-0 w-full h-full"
                :data-tip="item.label"
              ></div>
              <span v-if="item.child && !effectiveCompact" class="text-lg opacity-80 inline-flex items-center gap-1">
                <span class="opacity-30 text-sm">└</span>{{ item.icon }}
              </span>
              <span v-else class="text-lg opacity-80 w-6 text-center">{{ item.icon }}</span>
              <span v-show="!effectiveCompact" class="font-medium truncate">{{ item.label }}</span>
            </router-link>
          </li>
        </template>
      </ul>
    </nav>

    <!-- BOTTOM: theme toggle + account -->
    <div class="flex-none p-3 pt-0 flex flex-col gap-1 pad-safe-bottom">
      <div class="divider my-0"></div>

      <button
        @click="uiStore.toggleTheme"
        class="flex items-center gap-3 w-full p-2 rounded-lg hover:bg-base-200 transition-all text-sm"
        :class="{ 'justify-center': effectiveCompact }"
        aria-label="Toggle light/dark theme"
      >
        <span class="w-6 text-center text-lg">{{ uiStore.theme === 'dark' ? '☀️' : '🌙' }}</span>
        <span v-show="!effectiveCompact" class="font-medium">{{ uiStore.theme === 'dark' ? 'Light mode' : 'Dark mode' }}</span>
      </button>

      <!-- Expanded: account dropdown (opens upward over the nav). -->
      <div v-if="!effectiveCompact" class="dropdown dropdown-top w-full">
        <div
          tabindex="0"
          role="button"
          class="flex items-center gap-3 w-full p-2 rounded-lg bg-base-200/50 hover:bg-base-200 cursor-pointer transition-colors"
        >
          <div class="avatar placeholder">
            <div class="bg-neutral text-neutral-content rounded-full w-8">
              <span class="text-xs font-bold">{{ authStore.initial }}</span>
            </div>
          </div>
          <div class="flex flex-col truncate flex-1 text-left min-w-0">
            <span class="font-semibold text-sm truncate leading-tight">{{ roleLabel }}</span>
            <span class="text-xs text-base-content/60 truncate leading-tight">{{ authStore.email }}</span>
          </div>
          <span class="text-base-content/40 text-lg leading-none pr-1">⋮</span>
        </div>
        <ul tabindex="0" class="dropdown-content menu menu-sm bg-base-100 rounded-box shadow-lg border border-base-300 w-56 p-1 mb-1 z-50">
          <li><a class="text-error" @click="handleLogout">🚪 Sign out</a></li>
        </ul>
      </div>

      <!-- Compact: avatar + direct logout (the w-56 menu would overflow the rail). -->
      <template v-else>
        <div class="flex items-center justify-center w-full p-2 rounded-lg bg-base-200/50">
          <div class="avatar placeholder">
            <div class="bg-neutral text-neutral-content rounded-full w-8">
              <span class="text-xs font-bold">{{ authStore.initial }}</span>
            </div>
          </div>
        </div>
        <button
          @click="handleLogout"
          class="flex items-center justify-center w-full p-2 rounded-lg hover:bg-error/10 text-error transition-all"
          title="Sign out"
          aria-label="Sign out"
        >
          🚪
        </button>
      </template>
    </div>
  </aside>
</template>
