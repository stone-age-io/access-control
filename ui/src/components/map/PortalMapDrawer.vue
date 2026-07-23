<script setup lang="ts">
import type { Portal } from '@/types/pocketbase'
import SoftBadge from '@/components/ui/SoftBadge.vue'

defineProps<{ portal: Portal; isMobile: boolean }>()
defineEmits<{ close: [] }>()
</script>

<template>
  <div
    class="absolute top-0 bottom-0 right-0 z-[500] flex flex-col bg-base-100 border-l border-base-300 shadow-xl"
    :class="isMobile ? 'left-0 w-full border-l-0' : 'w-80'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-base-300 bg-base-200/30 shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <span class="text-lg shrink-0">🚪</span>
        <div class="min-w-0">
          <h3 class="font-bold text-sm truncate">{{ portal.name || portal.code }}</h3>
          <code class="text-xs text-primary">{{ portal.code }}</code>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-3 text-sm">
      <div v-if="portal.type" class="flex items-center gap-2">
        <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Type</span>
        <SoftBadge>{{ portal.type }}</SoftBadge>
      </div>
      <div v-if="portal.posture" class="flex items-center gap-2">
        <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Standing posture</span>
        <SoftBadge>{{ portal.posture }}</SoftBadge>
      </div>
    </div>

    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link :to="`/portals/${portal.id}`" class="btn btn-sm btn-block btn-outline">Open portal</router-link>
    </div>
  </div>
</template>
