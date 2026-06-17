<script setup lang="ts">
/** A labeled read-only value, for detail-view summary grids and rails. */
defineProps<{
  label: string
}>()
</script>

<template>
  <div class="min-w-0">
    <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-0.5">{{ label }}</div>
    <div class="text-sm break-words">
      <slot>
        <span class="opacity-40">—</span>
      </slot>
    </div>
  </div>
</template>
