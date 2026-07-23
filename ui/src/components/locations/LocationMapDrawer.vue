<script setup lang="ts">
import { ref, watch } from 'vue'
import { pb } from '@/utils/pb'
import type { Location, Portal } from '@/types/pocketbase'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const props = defineProps<{ location: Location; isMobile: boolean }>()
defineEmits<{ close: [] }>()

const portals = ref<Portal[]>([])
const loading = ref(false)

async function loadPortals(locationId: string) {
  loading.value = true
  try {
    portals.value = await pb
      .collection('portals')
      .getFullList<Portal>({ filter: `location = "${locationId}"`, sort: 'code' })
  } catch {
    portals.value = []
  } finally {
    loading.value = false
  }
}

watch(() => props.location.id, (id) => loadPortals(id), { immediate: true })
</script>

<template>
  <div
    class="absolute top-0 bottom-0 right-0 z-[500] flex flex-col bg-base-100 border-l border-base-300 shadow-xl"
    :class="isMobile ? 'left-0 w-full border-l-0' : 'w-80'"
  >
    <!-- Header -->
    <div class="flex items-center justify-between p-4 border-b border-base-300 bg-base-200/30 shrink-0">
      <div class="flex items-center gap-3 min-w-0">
        <span class="text-lg shrink-0">🏢</span>
        <div class="min-w-0">
          <h3 class="font-bold text-sm truncate">{{ location.name || location.code }}</h3>
          <code class="text-xs text-primary">{{ location.code }}</code>
        </div>
      </div>
      <button class="btn btn-sm btn-circle btn-ghost shrink-0" aria-label="Close" @click="$emit('close')">✕</button>
    </div>

    <div class="flex-1 overflow-y-auto">
      <!-- Meta -->
      <div class="p-4 border-b border-base-300 space-y-2">
        <p v-if="location.description" class="text-sm text-base-content/80">{{ location.description }}</p>
        <div class="flex items-center gap-2 text-xs">
          <span class="uppercase tracking-wider text-base-content/50 font-semibold">Timezone</span>
          <code class="font-mono">{{ location.timezone }}</code>
        </div>
      </div>

      <!-- Portals -->
      <div class="p-3">
        <div class="text-[10px] uppercase tracking-wider text-base-content/50 font-semibold mb-2">
          Portals
          <SoftBadge v-if="portals.length" class="ml-1">{{ portals.length }}</SoftBadge>
        </div>
        <div v-if="loading" class="flex justify-center p-4">
          <span class="loading loading-spinner loading-sm opacity-50"></span>
        </div>
        <p v-else-if="portals.length === 0" class="text-xs text-base-content/40 italic px-1 py-2">
          No portals in this location.
        </p>
        <router-link
          v-for="p in portals"
          :key="p.id"
          :to="`/portals/${p.id}`"
          class="w-full text-left p-2 rounded hover:bg-base-200 transition-colors flex items-center justify-between gap-2 min-w-0"
        >
          <span class="min-w-0 flex-1">
            <span class="font-medium text-sm truncate block">{{ p.name || p.code }}</span>
            <code class="text-xs text-primary">{{ p.code }}</code>
          </span>
          <SoftBadge v-if="p.type" class="shrink-0">{{ p.type }}</SoftBadge>
        </router-link>
      </div>
    </div>

    <!-- Footer -->
    <div class="p-3 border-t border-base-300 shrink-0">
      <router-link :to="`/locations/${location.id}`" class="btn btn-sm btn-block btn-outline">
        Open location
      </router-link>
    </div>
  </div>
</template>
