<script setup lang="ts">
/**
 * Base Card — simple wrapper around the DaisyUI card with consistent styling.
 */
defineProps<{
  title?: string
  noPadding?: boolean
}>()
</script>

<template>
  <div class="card bg-base-100 shadow-xl">
    <div v-if="title || $slots.header || $slots.actions" class="card-body pb-0">
      <div class="flex items-center justify-between">
        <h2 v-if="title" class="card-title">{{ title }}</h2>
        <div v-if="$slots.actions" class="flex items-center gap-2">
          <slot name="actions" />
        </div>
      </div>
      <slot name="header" />
    </div>
    <div v-if="!noPadding" class="card-body">
      <slot />
    </div>
    <slot v-else />
  </div>
</template>
