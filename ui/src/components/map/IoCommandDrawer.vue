<script setup lang="ts">
import { computed } from 'vue'
import type { AuxInput, AuxOutput, PointStatus } from '@/types/pocketbase'
import { statusKeyFor } from '@/utils/placeable'
import { useAuxCommands, auxStateBadge } from '@/composables/useAuxCommands'
import { useAuthStore } from '@/stores/auth'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// The Live Map "I/O" chip target: every aux input + output in this location with
// live state and (for outputs) inline drive controls. Mirrors AreaCommandDrawer —
// areas can't be markers; aux I/O can, so this is the list peer of the on-plan
// markers, one right-edge drawer at a time.
const props = defineProps<{
  auxInputs: AuxInput[]
  auxOutputs: AuxOutput[]
  statusByKey: Map<string, PointStatus>
  isMobile: boolean
}>()
defineEmits<{ close: [] }>()

const auth = useAuthStore()
const canCommand = computed(() => auth.can('command'))
const { commanding, drive } = useAuxCommands()

const total = computed(() => props.auxInputs.length + props.auxOutputs.length)

function inputBadge(a: AuxInput) {
  return auxStateBadge('aux_input', props.statusByKey.get(statusKeyFor('aux_input', a.code)))
}
function outputBadge(a: AuxOutput) {
  return auxStateBadge('aux_output', props.statusByKey.get(statusKeyFor('aux_output', a.code)))
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
        <span class="text-lg shrink-0">🔌</span>
        <div class="min-w-0">
          <h3 class="font-bold text-sm truncate">Aux I/O</h3>
          <span class="text-xs opacity-60">{{ total }} in this location</span>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <!-- Inputs -->
      <div v-if="auxInputs.length">
        <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2 flex items-center gap-1">
          🔌 Inputs
        </div>
        <div class="space-y-2">
          <div v-for="a in auxInputs" :key="a.id" class="rounded-lg border border-base-300 p-3 flex items-center justify-between gap-2">
            <router-link :to="`/aux-inputs/${a.id}`" class="min-w-0">
              <div class="font-medium text-sm truncate">{{ a.name || a.code }}</div>
              <code class="text-xs text-primary">{{ a.code }}</code>
            </router-link>
            <SoftBadge :tone="inputBadge(a).tone" dot class="shrink-0">{{ inputBadge(a).text }}</SoftBadge>
          </div>
        </div>
      </div>

      <!-- Outputs -->
      <div v-if="auxOutputs.length">
        <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2 flex items-center gap-1">
          🔆 Outputs
        </div>
        <div class="space-y-2">
          <div v-for="a in auxOutputs" :key="a.id" class="rounded-lg border border-base-300 p-3 space-y-2">
            <div class="flex items-center justify-between gap-2">
              <router-link :to="`/aux-outputs/${a.id}`" class="min-w-0">
                <div class="font-medium text-sm truncate">{{ a.name || a.code }}</div>
                <code class="text-xs text-primary">{{ a.code }}</code>
              </router-link>
              <SoftBadge :tone="outputBadge(a).tone" dot class="shrink-0">{{ outputBadge(a).text }}</SoftBadge>
            </div>
            <div v-if="canCommand" class="flex gap-2">
              <button class="btn btn-xs btn-outline btn-success flex-1" :disabled="commanding" @click="drive(a.id, 'on')">On</button>
              <button class="btn btn-xs btn-outline flex-1" :disabled="commanding" @click="drive(a.id, 'off')">Off</button>
              <button class="btn btn-xs btn-primary flex-1" :disabled="commanding" @click="drive(a.id, 'pulse')">
                Pulse
              </button>
            </div>
          </div>
        </div>
      </div>

      <p v-if="total === 0" class="text-sm opacity-50 text-center">No aux I/O in this location.</p>

      <p v-if="!canCommand && auxOutputs.length" class="text-xs opacity-50 pt-1">
        Read-only — driving outputs needs the <span class="font-medium">Commands</span> capability.
      </p>
    </div>

    <!-- Footer -->
    <div class="p-3 border-t border-base-300 shrink-0 grid grid-cols-2 gap-2">
      <router-link to="/aux-inputs" class="btn btn-sm btn-outline">Inputs</router-link>
      <router-link to="/aux-outputs" class="btn btn-sm btn-outline">Outputs</router-link>
    </div>
  </div>
</template>
