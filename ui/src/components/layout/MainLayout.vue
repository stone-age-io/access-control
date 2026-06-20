<script setup lang="ts">
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import HelpPanel from '@/components/ui/HelpPanel.vue'
</script>

<template>
  <div class="drawer lg:drawer-open h-dvh">
    <input id="sidebar-drawer" type="checkbox" class="drawer-toggle" />

    <!-- Main content -->
    <div class="drawer-content flex flex-col min-h-0">
      <AppHeader />
      <main class="flex-1 min-h-0 overflow-y-auto bg-base-200">
        <div class="mx-auto w-full max-w-7xl p-4 lg:p-6">
          <router-view />
        </div>
      </main>
    </div>

    <!-- Sidebar -->
    <div class="drawer-side z-40">
      <label for="sidebar-drawer" class="drawer-overlay"></label>
      <AppSidebar />
    </div>

    <!-- Contextual help: slide-over panel (opened from the header icon on mobile, the inline button on desktop) -->
    <HelpPanel />
  </div>
</template>
