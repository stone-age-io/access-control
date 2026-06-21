<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useUIStore } from '@/stores/ui'
import { useHelp } from '@/composables/useHelp'
import { helpForPath } from '@/help/registry'
import BrandLogo from '@/components/common/BrandLogo.vue'

const uiStore = useUIStore()
const route = useRoute()
const { open: openHelp } = useHelp()
const helpTopic = computed(() => helpForPath(route.path))
</script>

<template>
  <!-- Sticky header, mobile only (sidebar is permanent on lg+). A 3-column grid
       (1fr · auto · 1fr) keeps the logo dead-center no matter how many buttons
       flank it — the side columns are always equal width. -->
  <header class="navbar bg-base-100 border-b border-base-300 min-h-[4rem] lg:hidden sticky top-0 z-30 pad-safe-top">
    <div class="grid grid-cols-[1fr_auto_1fr] items-center w-full">
      <div class="justify-self-start">
        <label for="sidebar-drawer" class="btn btn-square btn-ghost" aria-label="Open navigation menu">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" class="inline-block w-6 h-6 stroke-current">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"></path>
          </svg>
        </label>
      </div>

      <router-link to="/" class="btn btn-ghost btn-circle p-1 justify-self-center">
        <span class="text-primary"><BrandLogo :size="32" /></span>
      </router-link>

      <div class="justify-self-end flex items-center">
        <button
          v-if="helpTopic"
          type="button"
          class="btn btn-square btn-ghost"
          :title="`Help: ${helpTopic.title}`"
          aria-label="Open help for this page"
          @click="openHelp"
        >
          <svg xmlns="http://www.w3.org/2000/svg" class="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </button>
        <button class="btn btn-square btn-ghost" aria-label="Toggle light/dark theme" @click="uiStore.toggleTheme">
          <span v-if="uiStore.theme === 'dark'" class="text-xl">☀️</span>
          <span v-else class="text-xl">🌙</span>
        </button>
      </div>
    </div>
  </header>
</template>
