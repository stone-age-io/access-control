<script setup lang="ts">
import { computed } from 'vue'
import type { AuxInput, AuxOutput, PointStatus } from '@/types/pocketbase'
import { statusKeyFor } from '@/utils/placeable'
import { useAuxCommands, auxStateBadge } from '@/composables/useAuxCommands'
import { useAuthStore } from '@/stores/auth'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// The "I/O" view on the Live Map — a responsive card grid peer of the Portals
// list: every aux input (observe-only) and output (with inline on/off/pulse).
// The list peer of the on-plan aux markers; output drive is `command`-gated.
const props = defineProps<{
  auxInputs: AuxInput[]
  auxOutputs: AuxOutput[]
  statusByKey: Map<string, PointStatus>
}>()

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
  <div class="p-4 space-y-5">
    <!-- Inputs -->
    <div v-if="auxInputs.length">
      <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2">🔌 Inputs</div>
      <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        <router-link
          v-for="a in auxInputs"
          :key="a.id"
          :to="`/aux-inputs/${a.id}`"
          class="rounded-lg border border-base-300 bg-base-100 p-3 flex items-center justify-between gap-2 hover:border-primary/40 transition-colors"
        >
          <span class="min-w-0">
            <span class="font-medium text-sm truncate block">{{ a.name || a.code }}</span>
            <code class="text-xs text-primary">{{ a.code }}</code>
          </span>
          <SoftBadge :tone="inputBadge(a).tone" dot class="shrink-0">{{ inputBadge(a).text }}</SoftBadge>
        </router-link>
      </div>
    </div>

    <!-- Outputs -->
    <div v-if="auxOutputs.length">
      <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-2">🔆 Outputs</div>
      <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        <div v-for="a in auxOutputs" :key="a.id" class="rounded-lg border border-base-300 bg-base-100 p-3 space-y-2">
          <div class="flex items-center justify-between gap-2">
            <router-link :to="`/aux-outputs/${a.id}`" class="min-w-0">
              <span class="font-medium text-sm truncate block">{{ a.name || a.code }}</span>
              <code class="text-xs text-primary">{{ a.code }}</code>
            </router-link>
            <SoftBadge :tone="outputBadge(a).tone" dot class="shrink-0">{{ outputBadge(a).text }}</SoftBadge>
          </div>
          <div v-if="canCommand" class="flex gap-2">
            <button class="btn btn-xs btn-outline btn-success flex-1" :disabled="commanding" @click="drive(a.id, 'on')">On</button>
            <button class="btn btn-xs btn-outline flex-1" :disabled="commanding" @click="drive(a.id, 'off')">Off</button>
            <button class="btn btn-xs btn-primary flex-1" :disabled="commanding" @click="drive(a.id, 'pulse')">Pulse</button>
          </div>
        </div>
      </div>
    </div>

    <p v-if="total === 0" class="text-sm opacity-50 text-center py-4">No aux I/O in this location.</p>
    <p v-if="!canCommand && auxOutputs.length" class="text-xs opacity-50">
      Read-only — driving outputs needs the <span class="font-medium">Commands</span> capability.
    </p>
  </div>
</template>
