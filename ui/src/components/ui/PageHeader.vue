<script setup lang="ts">
import HelpButton from './HelpButton.vue'

/**
 * The standard page header — title + optional subtitle on the left, an optional
 * `#actions` slot plus the contextual Help button on the right. Used by ListLayout
 * (and DetailLayout-style shells) and by standalone pages like the Live Map that
 * can't adopt the full list shell but must not drift from the shared header look.
 */
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
    <div>
      <h1 class="text-3xl font-bold">{{ title }}</h1>
      <p v-if="subtitle" class="text-base-content/70 mt-1">{{ subtitle }}</p>
    </div>
    <div class="flex items-center gap-2 w-full sm:w-auto">
      <slot name="actions" />
      <HelpButton />
    </div>
  </div>
</template>
