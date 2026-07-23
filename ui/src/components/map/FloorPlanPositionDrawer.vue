<script setup lang="ts">
import type { Portal } from '@/types/pocketbase'
import SoftBadge from '@/components/ui/SoftBadge.vue'

defineProps<{
  unmapped: Portal[]
  placed: Portal[]
  positionMode: boolean
  isMobile: boolean
}>()

const emit = defineEmits<{
  close: []
  'update:positionMode': [value: boolean]
  place: [portalId: string]
  unmap: [portalId: string]
}>()
</script>

<template>
  <div
    class="absolute top-0 bottom-0 right-0 z-[500] flex flex-col bg-base-100 border-l border-base-300 shadow-xl"
    :class="isMobile ? 'left-0 w-full border-l-0' : 'w-80'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-base-300 bg-base-200/30 shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <span class="text-lg shrink-0">🛠️</span>
        <h3 class="font-bold text-sm truncate">Position portals</h3>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="emit('close')">✕</button>
    </div>

    <!-- Drag toggle -->
    <label class="flex items-center justify-between gap-3 p-4 border-b border-base-300 cursor-pointer shrink-0">
      <span class="flex flex-col">
        <span class="text-sm font-medium">Drag to reposition</span>
        <span class="text-xs text-base-content/60">Enable dragging placed markers</span>
      </span>
      <input
        type="checkbox"
        class="toggle toggle-primary"
        :checked="positionMode"
        @change="emit('update:positionMode', ($event.target as HTMLInputElement).checked)"
      />
    </label>

    <div class="flex-1 overflow-y-auto">
      <!-- Not on plan -->
      <div class="p-3">
        <div class="text-[10px] uppercase tracking-wider text-base-content/50 font-semibold mb-2">
          Not on plan
          <SoftBadge v-if="unmapped.length" class="ml-1">{{ unmapped.length }}</SoftBadge>
        </div>
        <p v-if="unmapped.length === 0" class="text-xs text-base-content/40 italic px-1 py-2">All portals are placed.</p>
        <button
          v-for="p in unmapped"
          :key="p.id"
          class="w-full text-left p-2 rounded hover:bg-base-200 transition-colors flex items-center justify-between gap-2 min-w-0"
          @click="emit('place', p.id)"
        >
          <span class="min-w-0 flex-1">
            <span class="font-medium text-sm truncate block">{{ p.name || p.code }}</span>
            <SoftBadge v-if="p.type">{{ p.type }}</SoftBadge>
          </span>
          <span class="btn btn-xs btn-primary btn-outline shrink-0 pointer-events-none">Place</span>
        </button>
      </div>

      <!-- On plan -->
      <div class="p-3 border-t border-base-300">
        <div class="text-[10px] uppercase tracking-wider text-base-content/50 font-semibold mb-2">
          On plan
          <SoftBadge v-if="placed.length" class="ml-1">{{ placed.length }}</SoftBadge>
        </div>
        <p v-if="placed.length === 0" class="text-xs text-base-content/40 italic px-1 py-2">Nothing placed yet.</p>
        <div v-for="p in placed" :key="p.id" class="w-full p-2 rounded flex items-center justify-between gap-2 min-w-0">
          <span class="min-w-0 flex-1">
            <span class="font-medium text-sm truncate block">{{ p.name || p.code }}</span>
            <SoftBadge v-if="p.type">{{ p.type }}</SoftBadge>
          </span>
          <button class="btn btn-xs btn-ghost text-error shrink-0" @click="emit('unmap', p.id)">Remove</button>
        </div>
      </div>
    </div>
  </div>
</template>
