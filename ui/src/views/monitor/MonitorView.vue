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
  <div class="space-y-4">
    <div>
      <h1 class="text-3xl font-bold">Live Map</h1>
      <p class="text-base-content/70 mt-1">Monitor doors and send commands in real time.</p>
    </div>

    <OperationalFloorplan v-if="locationId" :key="locationId" :location-id="locationId" />
    <LocationMapViz v-else drill-in @select="goToLocation" />
  </div>
</template>
