<script setup lang="ts">
import { computed } from 'vue'
import type { Area, PointStatus } from '@/types/pocketbase'
import { useAreaCommands } from '@/composables/useAreaCommands'
import { useAuthStore } from '@/stores/auth'
import { aggregateArm, armTone, armLabel } from '@/utils/arming'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// The "Areas" view on the Live Map — a responsive card grid peer of the Portals
// list (areas have no floor-plan position, so they can't be markers). Arm/disarm
// controls are inline; gated by the `command` capability.
// `flush` drops the outer padding when embedded in a section that pads itself
// (the all-locations overview); standalone as a view it pads itself.
const props = defineProps<{ areas: Area[]; shadows: Map<string, PointStatus[]>; flush?: boolean }>()

const auth = useAuthStore()
const canCommand = computed(() => auth.can('command'))
const { commanding, arm, disarm, armClear } = useAreaCommands()

function armFor(a: Area) {
  return aggregateArm(props.shadows.get(a.code) ?? [])
}
</script>

<template>
  <div :class="flush ? '' : 'p-4'">
    <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
      <div v-for="a in areas" :key="a.id" class="rounded-lg border border-base-300 bg-base-100 p-3 space-y-2">
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
      <p v-if="areas.length === 0" class="text-sm opacity-50 col-span-full text-center py-4">
        No areas in this location.
      </p>
    </div>
    <p v-if="!canCommand && areas.length" class="text-xs opacity-50 mt-3">
      Read-only — arming needs the <span class="font-medium">Commands</span> capability.
    </p>
  </div>
</template>
