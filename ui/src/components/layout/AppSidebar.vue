<script setup lang="ts">
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { useAuthStore } from '@/stores/auth'
import { useUIStore } from '@/stores/ui'
import BrandLogo from '@/components/common/BrandLogo.vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const uiStore = useUIStore()

const isLargeScreen = useMediaQuery('(min-width: 1024px)')
const effectiveCompact = computed(() => uiStore.sidebarCompact && isLargeScreen.value)

interface NavItem { label: string; icon: string; path: string }
interface NavSection { title?: string; items: NavItem[] }

const sections: NavSection[] = [
  { items: [{ label: 'Overview', icon: '📊', path: '/' }] },
  {
    title: 'Topology',
    items: [
      { label: 'Sites', icon: '🏢', path: '/sites' },
      { label: 'Schedules', icon: '🗓️', path: '/schedules' },
      { label: 'Access Points', icon: '🚪', path: '/access-points' },
    ],
  },
  {
    title: 'Authorization',
    items: [
      { label: 'Access Groups', icon: '🗝️', path: '/access-groups' },
      { label: 'Roles', icon: '🛡️', path: '/roles' },
    ],
  },
  {
    title: 'People',
    items: [
      { label: 'Cardholders', icon: '🪪', path: '/cardholders' },
      { label: 'Credentials', icon: '🎫', path: '/credentials' },
    ],
  },
  {
    title: 'Activity',
    items: [{ label: 'Events', icon: '📋', path: '/events' }],
  },
]

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(path + '/')
}

function closeDrawer() {
  const drawer = document.getElementById('sidebar-drawer') as HTMLInputElement | null
  if (drawer) drawer.checked = false
}

async function handleLogout() {
  await authStore.logout()
  closeDrawer()
  router.push('/login')
}
</script>

<template>
  <aside
    class="bg-base-100 h-screen flex flex-col border-r border-base-300 transition-all duration-300 ease-in-out z-20"
    :class="effectiveCompact ? 'w-20 min-w-[5rem]' : 'w-72 min-w-[18rem]'"
  >
    <!-- TOP: brand + collapse toggle -->
    <div class="flex-none p-3 pb-0">
      <div
        class="flex transition-all duration-300"
        :class="effectiveCompact ? 'flex-col items-center gap-3 py-2' : 'flex-row items-center justify-between px-2 py-2'"
      >
        <router-link to="/" class="flex items-center gap-3 hover:opacity-80 transition-opacity overflow-hidden" @click="closeDrawer">
          <div class="w-10 h-10 flex items-center justify-center flex-shrink-0 text-primary">
            <BrandLogo :size="36" />
          </div>
          <span v-show="!effectiveCompact" class="font-bold text-lg tracking-tight whitespace-nowrap overflow-hidden">
            Stone Access
          </span>
        </router-link>

        <button
          v-if="isLargeScreen"
          @click="uiStore.toggleCompact"
          class="btn btn-ghost btn-sm btn-square opacity-60 hover:opacity-100 transition-opacity"
          :title="uiStore.sidebarCompact ? 'Expand sidebar' : 'Collapse sidebar'"
        >
          <span v-if="uiStore.sidebarCompact">»</span>
          <span v-else>«</span>
        </button>
      </div>
      <div class="divider my-0"></div>
    </div>

    <!-- NAVIGATION -->
    <nav class="flex-1 overflow-y-auto overflow-x-hidden px-3 pb-2">
      <ul class="menu p-0 gap-1 w-full">
        <template v-for="(section, si) in sections" :key="si">
          <li v-if="section.title && !effectiveCompact" class="menu-title px-2 pt-3 pb-1 text-[10px] uppercase tracking-widest opacity-50">
            {{ section.title }}
          </li>
          <li v-else-if="section.title && effectiveCompact" class="py-1">
            <div class="divider my-0"></div>
          </li>

          <li v-for="item in section.items" :key="item.path">
            <router-link
              :to="item.path"
              :class="{ active: isActive(item.path) }"
              class="group relative"
              @click="closeDrawer"
            >
              <div
                v-if="effectiveCompact"
                class="tooltip tooltip-right absolute left-0 w-full h-full"
                :data-tip="item.label"
              ></div>
              <span class="text-lg opacity-80 w-6 text-center">{{ item.icon }}</span>
              <span v-show="!effectiveCompact" class="font-medium truncate">{{ item.label }}</span>
            </router-link>
          </li>
        </template>
      </ul>
    </nav>

    <!-- BOTTOM: theme + user + logout -->
    <div class="flex-none p-3 pt-0 flex flex-col gap-1">
      <div class="divider my-0"></div>

      <button
        @click="uiStore.toggleTheme"
        class="flex items-center gap-3 w-full p-2 rounded-lg hover:bg-base-200 transition-all text-sm"
        :class="{ 'justify-center': effectiveCompact }"
      >
        <span class="w-6 text-center text-lg">{{ uiStore.theme === 'dark' ? '☀️' : '🌙' }}</span>
        <span v-show="!effectiveCompact" class="font-medium">{{ uiStore.theme === 'dark' ? 'Light mode' : 'Dark mode' }}</span>
      </button>

      <div
        class="flex items-center gap-3 w-full p-2 rounded-lg bg-base-200/50 border border-transparent"
        :class="{ 'justify-center': effectiveCompact }"
      >
        <div class="avatar placeholder">
          <div class="bg-neutral text-neutral-content rounded-full w-8">
            <span class="text-xs font-bold">{{ authStore.initial }}</span>
          </div>
        </div>
        <div v-show="!effectiveCompact" class="flex flex-col truncate flex-1 text-left min-w-0">
          <span class="font-semibold text-sm truncate leading-tight">Superuser</span>
          <span class="text-xs text-base-content/60 truncate leading-tight">{{ authStore.email }}</span>
        </div>
        <button
          v-show="!effectiveCompact"
          @click="handleLogout"
          class="btn btn-ghost btn-xs text-error"
          title="Log out"
        >
          🚪
        </button>
      </div>

      <button
        v-if="effectiveCompact"
        @click="handleLogout"
        class="flex items-center justify-center w-full p-2 rounded-lg hover:bg-error/10 text-error transition-all"
        title="Log out"
      >
        🚪
      </button>
    </div>
  </aside>
</template>
