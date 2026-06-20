<script setup lang="ts">
/** Empty-collection placeholder, optionally with a call-to-action via #action. */
import BaseCard from './BaseCard.vue'

defineProps<{
  icon?: string
  title: string
  message?: string
}>()
</script>

<template>
  <BaseCard>
    <div class="text-center py-12">
      <span class="text-6xl">{{ icon || '📭' }}</span>
      <h3 class="text-xl font-bold mt-4">{{ title }}</h3>
      <p v-if="message" class="text-base-content/70 mt-2">{{ message }}</p>
      <div v-if="$slots.action" class="mt-4">
        <slot name="action" />
      </div>
    </div>
  </BaseCard>
</template>
