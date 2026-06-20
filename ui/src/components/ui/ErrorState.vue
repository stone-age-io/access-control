<script setup lang="ts">
/** Load-failure placeholder with a Retry button. */
import BaseCard from './BaseCard.vue'

defineProps<{
  title?: string
  message?: string
}>()

defineEmits<{ retry: [] }>()
</script>

<template>
  <BaseCard>
    <div class="text-center py-12">
      <span class="text-6xl">&#9888;</span>
      <h3 class="text-xl font-bold mt-4">{{ title || 'Something went wrong' }}</h3>
      <p v-if="message" class="text-base-content/70 mt-2">{{ message }}</p>
      <button class="btn btn-primary mt-4" @click="$emit('retry')">Retry</button>
    </div>
  </BaseCard>
</template>
