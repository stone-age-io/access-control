<script setup lang="ts">
import { computed } from 'vue'
import type { Area, PointStatus } from '@/types/pocketbase'
import { useAreaCommands } from '@/composables/useAreaCommands'
import { useAuthStore } from '@/stores/auth'
import { aggregateArm, armTone, armLabel } from '@/utils/arming'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// Areas have no floor-plan coordinates, so unlike portals they can't be markers.
// They ride the SAME right-edge drawer as PortalCommandDrawer instead of floating
// over the canvas — reached from the context-bar "Areas" chip, one open at a time.
const props = defineProps<{ areas: Area[]; shadows: Map<string, PointStatus[]>; isMobile: boolean }>()
defineEmits<{ close: [] }>()

const auth = useAuthStore()
const canCommand = computed(() => auth.can('command'))
const { commanding, arm, disarm, armClear } = useAreaCommands()

// Live aggregated arm-state for an area, from its per-controller shadows.
function armFor(a: Area) {
  return aggregateArm(props.shadows.get(a.code) ?? [])
}
</script>

<template>
  <div
    class="absolute top-0 bottom-0 right-0 z-[500] flex flex-col bg-base-100 border-l border-base-300 shadow-xl"
    :class="isMobile ? 'left-0 w-full border-l-0' : 'w-80'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-base-300 bg-base-200/30 shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <span class="text-lg shrink-0">🛡️</span>
        <div class="min-w-0">
          <h3 class="font-bold text-sm truncate">Areas</h3>
          <span class="text-xs opacity-60">{{ areas.length }} in this location</span>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-3">
      <div v-for="a in areas" :key="a.id" class="rounded-lg border border-base-300 p-3 space-y-2">
        <div class="flex items-center justify-between gap-2">
          <div class="min-w-0">
            <div class="font-medium text-sm truncate">{{ a.name || a.code }}</div>
            <code class="text-xs text-primary">{{ a.code }}</code>
          </div>
          <SoftBadge :tone="armTone(armFor(a).state)" dot class="shrink-0">{{ armLabel(armFor(a)) }}</SoftBadge>
        </div>
        <div v-if="canCommand" class="flex gap-2">
          <button class="btn btn-xs btn-warning flex-1" :disabled="commanding" @click="arm(a.id, a.code)">Arm</button>
          <button class="btn btn-xs flex-1" :disabled="commanding" @click="disarm(a.id, a.code)">Disarm</button>
          <button
            class="btn btn-xs btn-ghost"
            :disabled="commanding || !a.arm_override"
            title="Clear override"
            @click="armClear(a.id)"
          >
            Clear
          </button>
        </div>
      </div>

      <p v-if="areas.length === 0" class="text-sm opacity-50 text-center">No areas in this location.</p>

      <p v-if="!canCommand && areas.length" class="text-xs opacity-50 pt-1">
        Read-only — arming needs the <span class="font-medium">Door commands</span> capability.
      </p>
    </div>

    <!-- Footer -->
    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link to="/areas" class="btn btn-sm btn-block btn-outline">Manage areas</router-link>
    </div>
  </div>
</template>
