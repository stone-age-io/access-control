<script setup lang="ts">
import { computed } from 'vue'
import type { Placeable } from '@/utils/placeable'
import { PLACE_KIND_META, PLACE_KIND_ROUTE } from '@/utils/placeable'
import SoftBadge from '@/components/ui/SoftBadge.vue'

// Editor-side (view mode) detail for any placed marker — portal or aux I/O.
// Informational only; placement is the map's job and live control lives on the
// operational Live Map. `record` is the underlying PB row (fields read by kind).
const props = defineProps<{ placeable: Placeable; record: any; isMobile: boolean }>()
defineEmits<{ close: [] }>()

const meta = computed(() => PLACE_KIND_META[props.placeable.kind])
const to = computed(() => `${PLACE_KIND_ROUTE[props.placeable.kind]}/${props.placeable.recordId}`)
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
          <h3 class="font-bold text-sm truncate">{{ placeable.name || placeable.code }}</h3>
          <code class="text-xs text-primary">{{ placeable.code }}</code>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto p-4 space-y-3 text-sm">
      <div class="flex items-center gap-2">
        <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Kind</span>
        <SoftBadge>{{ meta.label }}</SoftBadge>
      </div>

      <!-- Portal specifics -->
      <template v-if="placeable.kind === 'portal'">
        <div v-if="record?.type" class="flex items-center gap-2">
          <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Type</span>
          <SoftBadge>{{ record.type }}</SoftBadge>
        </div>
        <div v-if="record?.posture" class="flex items-center gap-2">
          <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Standing posture</span>
          <SoftBadge>{{ record.posture }}</SoftBadge>
        </div>
      </template>

      <!-- Aux input specifics -->
      <div v-else-if="placeable.kind === 'aux_input'" class="flex items-center gap-2">
        <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Point type</span>
        <SoftBadge>{{ record?.point_type || 'monitor' }}</SoftBadge>
      </div>

      <!-- Aux output specifics -->
      <div v-else-if="placeable.kind === 'aux_output'" class="flex items-center gap-2">
        <span class="uppercase tracking-wider text-base-content/50 font-semibold text-xs">Pulse</span>
        <SoftBadge>{{ record?.pulse_seconds ?? 0 }} s</SoftBadge>
      </div>
    </div>

    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link :to="to" class="btn btn-sm btn-block btn-outline">Open {{ meta.label.toLowerCase() }}</router-link>
    </div>
  </div>
</template>
