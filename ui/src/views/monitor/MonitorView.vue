<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Location } from '@/types/pocketbase'
import { useUIStore } from '@/stores/ui'
import LocationMapViz from '@/components/locations/LocationMapViz.vue'
import OperationalFloorplan from '@/views/monitor/OperationalFloorplan.vue'
import OverviewList from '@/views/monitor/OverviewList.vue'
import PageHeader from '@/components/ui/PageHeader.vue'

const route = useRoute()
const router = useRouter()
const ui = useUIStore()

// /monitor → all-locations overview; /monitor/:locationId → that building's live view.
const locationId = computed(() => (route.params.locationId as string) || '')

type OverviewMode = 'map' | 'list'
function isOverview(v: unknown): v is OverviewMode {
  return v === 'map' || v === 'list'
}
// All-locations overview: geographic map or grouped list. Deep link (?overview=)
// wins on first load, else the persisted preference.
const overviewMode = ref<OverviewMode>(isOverview(route.query.overview) ? route.query.overview : ui.monitorOverviewMode)

function setOverview(m: OverviewMode) {
  overviewMode.value = m
  ui.setMonitorOverviewMode(m)
  router.replace({ path: route.path, query: { ...route.query, overview: m } }).catch(() => {})
}

function goToLocation(loc: Location) {
  router.push(`/monitor/${loc.id}`)
}
</script>

<template>
  <!-- Fill the viewport: the page is exactly the shell's content area tall, so the
       map fills the screen without a page scrollbar. Height = 100dvh minus the
       shell chrome above/around <main> — on lg+ the header is hidden and only the
       wrapper's lg:p-6 (3rem) remains; below lg add the 4rem header + p-4 (2rem).
       The title is auto; the content wrapper takes the rest (flex-1). -->
  <div class="flex flex-col gap-4 h-[calc(100dvh-6rem)] lg:h-[calc(100dvh-3rem)]">
    <PageHeader
      class="shrink-0"
      title="Live View"
      subtitle="Monitor and command portals, areas, and aux I/O in real time."
    >
      <template #actions>
        <!-- All-locations view toggle (hidden once drilled into a building). -->
        <div v-if="!locationId" class="join">
          <button
            class="join-item btn btn-sm gap-1"
            :class="overviewMode === 'map' ? 'btn-active btn-primary' : ''"
            @click="setOverview('map')"
          >
            🗺️ <span class="hidden sm:inline">Map</span>
          </button>
          <button
            class="join-item btn btn-sm gap-1"
            :class="overviewMode === 'list' ? 'btn-active btn-primary' : ''"
            @click="setOverview('list')"
          >
            ☰ <span class="hidden sm:inline">List</span>
          </button>
        </div>
      </template>
    </PageHeader>

    <div class="flex-1 min-h-0">
      <OperationalFloorplan v-if="locationId" :key="locationId" :location-id="locationId" class="h-full" />
      <OverviewList v-else-if="overviewMode === 'list'" class="h-full" />
      <LocationMapViz v-else drill-in fill @select="goToLocation" />
    </div>
  </div>
</template>
