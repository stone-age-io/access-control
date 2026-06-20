<script setup lang="ts">
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import HelpPanel from '@/components/ui/HelpPanel.vue'
</script>

<template>
  <div class="drawer lg:drawer-open">
    <input id="sidebar-drawer" type="checkbox" class="drawer-toggle" />

    <!-- Main content -->
    <div class="drawer-content flex flex-col">
      <AppHeader />
      <main class="flex-1 overflow-y-auto p-4 lg:p-6 bg-base-200">
        <router-view />
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
