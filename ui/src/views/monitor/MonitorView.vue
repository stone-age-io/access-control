<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Location } from '@/types/pocketbase'
import LocationMapViz from '@/components/locations/LocationMapViz.vue'
import OperationalFloorplan from '@/views/monitor/OperationalFloorplan.vue'

const route = useRoute()
const router = useRouter()

// /monitor → geographic overview; /monitor/:locationId → that building's live floor plan.
const locationId = computed(() => (route.params.locationId as string) || '')

function goToLocation(loc: Location) {
  router.push(`/monitor/${loc.id}`)
}
</script>

<template>
  <!-- Fill the viewport: the page is exactly the shell's content area tall, so the
       map fills the screen without a page scrollbar. Height = 100dvh minus the
       shell chrome above/around <main> — on lg+ the header is hidden and only the
       wrapper's lg:p-6 (3rem) remains; below lg add the 4rem header + p-4 (2rem).
       The title is auto; the map wrapper takes the rest (flex-1). Both the geo map
       and the floor plan fill this same wrapper, so they render at the same size. -->
  <div class="flex flex-col gap-4 h-[calc(100dvh-6rem)] lg:h-[calc(100dvh-3rem)]">
    <div class="shrink-0">
      <h1 class="text-3xl font-bold">Live Map</h1>
      <p class="text-base-content/70 mt-1">Monitor doors and send commands in real time.</p>
    </div>

    <div class="flex-1 min-h-0">
      <OperationalFloorplan v-if="locationId" :key="locationId" :location-id="locationId" class="h-full" />
      <LocationMapViz v-else drill-in fill @select="goToLocation" />
    </div>
  </div>
</template>
