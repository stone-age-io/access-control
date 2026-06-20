<script setup lang="ts">
import { useUIStore } from '@/stores/ui'
import BrandLogo from '@/components/common/BrandLogo.vue'

const uiStore = useUIStore()
</script>

<template>
  <!-- Sticky header, mobile only (sidebar is permanent on lg+). -->
  <header class="navbar bg-base-100 border-b border-base-300 min-h-[4rem] lg:hidden sticky top-0 z-30">
    <div class="flex-none">
      <label for="sidebar-drawer" class="btn btn-square btn-ghost" aria-label="Open navigation menu">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" class="inline-block w-6 h-6 stroke-current">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"></path>
        </svg>
      </label>
    </div>

    <div class="flex-1 flex justify-center">
      <router-link to="/" class="btn btn-ghost btn-circle p-1">
        <span class="text-primary"><BrandLogo :size="32" /></span>
      </router-link>
    </div>

    <div class="flex-none">
      <button class="btn btn-square btn-ghost" aria-label="Toggle light/dark theme" @click="uiStore.toggleTheme">
        <span v-if="uiStore.theme === 'dark'" class="text-xl">☀️</span>
        <span v-else class="text-xl">🌙</span>
      </button>
    </div>
  </header>
</template>
