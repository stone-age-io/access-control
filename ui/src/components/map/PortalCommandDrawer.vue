<script setup lang="ts">
import { computed } from 'vue'
import type { Portal, PointStatus } from '@/types/pocketbase'
import { usePortalCommands, POSTURES } from '@/composables/usePortalCommands'
import { useAuthStore } from '@/stores/auth'
import PostureBadge from '@/components/ui/PostureBadge.vue'

const props = defineProps<{ portal: Portal; status: PointStatus | null; isMobile: boolean }>()
defineEmits<{ close: [] }>()

const auth = useAuthStore()
const canCommand = computed(() => auth.can('command'))
const { commanding, grant, setPosture } = usePortalCommands()

const isOverridden = computed(() => props.status?.posture_source === 'override')

const doorBadge = computed(() => {
  switch (props.status?.state) {
    case 'open':
      return { cls: 'badge-error', text: 'Open' }
    case 'closed':
      return { cls: 'badge-success', text: 'Closed' }
    default:
      return { cls: 'badge-ghost', text: 'Unknown' }
  }
})
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

    <div class="flex-1 overflow-y-auto p-4 space-y-4">
      <!-- Live status -->
      <div class="space-y-3">
        <div class="flex items-center justify-between gap-2">
          <span class="text-[10px] uppercase tracking-wider opacity-50 font-semibold">Posture</span>
          <PostureBadge v-if="status" :posture="status.posture" :source="status.posture_source" />
          <span v-else class="opacity-40 text-sm">—</span>
        </div>
        <div class="grid grid-cols-2 gap-2 text-center">
          <div>
            <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-1">Door</div>
            <span class="badge badge-sm" :class="doorBadge.cls">{{ doorBadge.text }}</span>
          </div>
          <div>
            <div class="text-[10px] uppercase tracking-wider opacity-50 font-semibold mb-1">Held</div>
            <span v-if="status?.held" class="badge badge-sm badge-warning">Held</span>
            <span v-else class="opacity-40 text-sm">No</span>
          </div>
        </div>
      </div>
      <p v-if="!status" class="text-xs opacity-50 text-center">
        No live status — the controller driving this portal hasn’t reported.
      </p>

      <!-- Momentary -->
      <div v-if="canCommand" class="border-t border-base-200 pt-4">
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Momentary</div>
        <button class="btn btn-sm btn-primary btn-block" :disabled="commanding" @click="grant(portal.id)">
          Grant (unlock once)
        </button>
      </div>

      <!-- Posture override -->
      <div v-if="canCommand" class="border-t border-base-200 pt-4">
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-2">Posture override</div>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="p in POSTURES"
            :key="p.value"
            class="btn btn-sm"
            :class="p.danger ? 'btn-outline btn-warning' : 'btn-outline'"
            :disabled="commanding"
            @click="setPosture(portal.id, p.value, { danger: p.danger, code: portal.code })"
          >
            {{ p.label }}
          </button>
          <button
            class="btn btn-sm"
            :class="isOverridden ? 'btn-outline btn-warning' : 'btn-ghost'"
            :disabled="commanding || !isOverridden"
            @click="setPosture(portal.id, 'clear')"
          >
            Clear override
          </button>
        </div>
        <p class="text-xs opacity-50 mt-2">
          <span v-if="isOverridden" class="text-warning font-medium">A manual override is in force. </span>
          An override is operational state on the controller — not saved to the record. “Clear” reverts to the
          scheduled or standing posture.
        </p>
      </div>

      <p v-if="!canCommand" class="border-t border-base-200 pt-4 text-xs opacity-50">
        Read-only — issuing commands needs the <span class="font-medium">Door commands</span> capability.
      </p>
    </div>

    <!-- Footer -->
    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link :to="`/portals/${portal.id}`" class="btn btn-sm btn-block btn-outline">Open portal</router-link>
    </div>
  </div>
</template>
