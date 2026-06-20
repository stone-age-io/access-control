<script setup lang="ts">
/**
 * Inline help trigger — desktop only. Sits next to the page actions
 * (Edit/Delete on detail views, "New X" on lists). Self-hides when the
 * current route has no help entry. Mobile uses the AppHeader help icon instead.
 */
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useHelp } from '@/composables/useHelp'
import { helpForPath } from '@/help/registry'

const route = useRoute()
const { open } = useHelp()
const topic = computed(() => helpForPath(route.path))
</script>

<template>
  <button
    v-if="topic"
    type="button"
    class="btn btn-sm btn-ghost gap-1.5 hidden lg:inline-flex"
    :title="`Help: ${topic.title}`"
    aria-label="Open help for this page"
    @click="open"
  >
    <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 opacity-70" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
      <path stroke-linecap="round" stroke-linejoin="round" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
    </svg>
    <span>Help</span>
  </button>
</template>
