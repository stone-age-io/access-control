<script setup lang="ts">
/**
 * List footer: a count on the left (via #default slot) and prev/next paging on
 * the right. The pager hides when there's only one page.
 */
defineProps<{
  page: number
  totalPages: number
  loading?: boolean
}>()

defineEmits<{ prev: []; next: [] }>()
</script>

<template>
  <div class="flex flex-col sm:flex-row justify-between items-center gap-4 p-4 border-t border-base-300">
    <span class="text-sm text-base-content/60"><slot /></span>
    <div v-if="totalPages > 1" class="join">
      <button class="join-item btn btn-sm" :disabled="page === 1 || loading" aria-label="Previous page" @click="$emit('prev')">«</button>
      <button class="join-item btn btn-sm" aria-current="page">{{ page }} / {{ totalPages }}</button>
      <button class="join-item btn btn-sm" :disabled="page === totalPages || loading" aria-label="Next page" @click="$emit('next')">»</button>
    </div>
  </div>
</template>
