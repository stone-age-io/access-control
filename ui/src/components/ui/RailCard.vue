<script setup lang="ts">
/** A compact titled card for the detail/form right rail. */
defineProps<{
  title?: string
  icon?: string
}>()
</script>

<template>
  <div class="card bg-base-100 shadow-md border border-base-300/60">
    <div class="card-body p-4 gap-3">
      <h3
        v-if="title"
        class="text-xs font-bold uppercase tracking-wider opacity-60 flex items-center gap-2"
      >
        <span v-if="icon">{{ icon }}</span>{{ title }}
      </h3>
      <slot />
    </div>
  </div>
</template>
