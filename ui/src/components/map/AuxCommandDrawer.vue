<script setup lang="ts">
import { computed } from 'vue'
import type { AuxInput, AuxOutput, PointStatus } from '@/types/pocketbase'
import { PLACE_KIND_META } from '@/utils/placeable'
import { useAuxCommands, auxStateBadge } from '@/composables/useAuxCommands'
import { useAuthStore } from '@/stores/auth'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// Single aux point on the Live Map (marker click). Inputs are observe-only;
// outputs add on/off/pulse. Shares the right-edge drawer with the portal and
// area drawers — one open at a time.
const props = defineProps<{
  kind: 'aux_input' | 'aux_output'
  record: AuxInput | AuxOutput
  status: PointStatus | null
  isMobile: boolean
}>()
defineEmits<{ close: [] }>()

const auth = useAuthStore()
const canCommand = computed(() => auth.can('command'))
const { commanding, drive } = useAuxCommands()

const meta = computed(() => PLACE_KIND_META[props.kind])
const badge = computed(() => auxStateBadge(props.kind, props.status))
const isOutput = computed(() => props.kind === 'aux_output')
const pulseSeconds = computed(() => (isOutput.value ? (props.record as AuxOutput).pulse_seconds : 0))
const to = computed(() => (isOutput.value ? `/aux-outputs/${props.record.id}` : `/aux-inputs/${props.record.id}`))
</script>

<template>
  <div
    class="absolute top-0 bottom-0 right-0 z-[500] flex flex-col bg-base-100 border-l border-base-300 shadow-xl"
    :class="isMobile ? 'left-0 w-full border-l-0' : 'w-80'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-base-300 bg-base-200/30 shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <span class="text-lg shrink-0">{{ meta.emoji }}</span>
        <div class="min-w-0">
          <h3 class="font-bold text-sm truncate">{{ record.name || record.code }}</h3>
          <code class="text-xs text-primary">{{ record.code }}</code>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <!-- Live status -->
      <div class="flex items-center justify-between gap-2">
        <span class="text-[10px] uppercase tracking-wider opacity-50 font-semibold">{{ meta.label }}</span>
        <SoftBadge :tone="badge.tone" dot>{{ badge.text }}</SoftBadge>
      </div>
      <p v-if="!status" class="text-xs opacity-50">
        No live status — the controller driving this point hasn’t reported.
      </p>

      <!-- Output controls -->
      <div v-if="isOutput && canCommand" class="border-t border-base-200 pt-4">
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Drive</div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-sm btn-outline btn-success" :disabled="commanding" @click="drive(record.id, 'on')">On</button>
          <button class="btn btn-sm btn-outline" :disabled="commanding" @click="drive(record.id, 'off')">Off</button>
          <button class="btn btn-sm btn-primary" :disabled="commanding" @click="drive(record.id, 'pulse')">
            Pulse ({{ pulseSeconds }}s)
          </button>
        </div>
        <p class="text-xs opacity-50 mt-2">On/Off set the standing state; Pulse energizes momentarily.</p>
      </div>

      <p v-else-if="isOutput && !canCommand" class="border-t border-base-200 pt-4 text-xs opacity-50">
        Read-only — driving this output needs the <span class="font-medium">Commands</span> capability.
      </p>

      <p v-else class="border-t border-base-200 pt-4 text-xs opacity-50">
        Observe-only input — surfaced live, no controls.
      </p>
    </div>

    <!-- Footer -->
    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link :to="to" class="btn btn-sm btn-block btn-outline">Open {{ meta.label.toLowerCase() }}</router-link>
    </div>
  </div>
</template>
