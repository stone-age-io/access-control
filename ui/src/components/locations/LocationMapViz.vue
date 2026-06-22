<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { pb } from '@/utils/pb'
import { useUIStore } from '@/stores/ui'
import { useLeafletMap, type MapMarkerInput } from '@/composables/useLeafletMap'
import type { Location } from '@/types/pocketbase'
import LocationMapDrawer from '@/components/locations/LocationMapDrawer.vue'

const props = withDefaults(defineProps<{ searchQuery?: string; drillIn?: boolean; fill?: boolean }>(), {
  searchQuery: '',
  drillIn: false,
  // `fill`: size to the parent (h-full, no 600px floor) instead of the standalone
  // 600px-min used in the locations list. The Live Map sets this so the map fills
  // its viewport-height wrapper exactly — on mobile the floor would otherwise
  // overflow the shorter wrapper and force a page scrollbar.
  fill: false,
})

// In drill-in mode (the operational Monitor), a marker click hands the location
// up to the parent (which navigates into its live floor plan) instead of opening
// the inline config drawer.
const emit = defineEmits<{ select: [location: Location] }>()

const uiStore = useUIStore()
const { initMap, renderMarkers, setSelectedMarker, updateTheme, fitAllMarkers, invalidateSize, cleanup } =
  useLeafletMap()

const loading = ref(true)
const locations = ref<Location[]>([])
const selectedLocation = ref<Location | null>(null)
const isMobile = ref(false)
const mapContainerId = 'location-list-map-container'

function isMapped(l: Location): boolean {
  const lat = l.coordinates?.lat ?? 0
  const lon = l.coordinates?.lon ?? 0
  return lat !== 0 || lon !== 0
}

const filteredLocations = computed(() => {
  const q = props.searchQuery.toLowerCase().trim()
  if (!q) return locations.value
  return locations.value.filter(
    (l) =>
      l.name?.toLowerCase().includes(q) ||
      l.code?.toLowerCase().includes(q) ||
      l.description?.toLowerCase().includes(q),
  )
})

function checkMobile() {
  isMobile.value = window.innerWidth < 768
}

// On the Live Map the height tracks the viewport, so a window resize changes the
// container — tell Leaflet to recompute its layout (no-op before the map inits).
function onResize() {
  checkMobile()
  invalidateSize()
}

function toMarkers(locs: Location[]): MapMarkerInput[] {
  return locs.map((l) => ({
    id: l.id,
    lat: l.coordinates.lat,
    lon: l.coordinates.lon,
    label: l.name || l.code,
  }))
}

function handleMarkerClick(id: string) {
  const loc = locations.value.find((l) => l.id === id)
  if (!loc) return
  if (props.drillIn) {
    emit('select', loc)
    return
  }
  if (selectedLocation.value?.id === id) {
    closeDrawer()
    return
  }
  selectedLocation.value = loc
  setSelectedMarker(id)
  if (!isMobile.value) nextTick(() => invalidateSize())
}

function handleMapClick(event: MouseEvent) {
  if (isMobile.value || !selectedLocation.value) return
  const target = event.target as HTMLElement
  if (target.closest('.leaflet-marker-icon') || target.closest('.location-map-drawer')) return
  closeDrawer()
}

function closeDrawer() {
  selectedLocation.value = null
  setSelectedMarker(null)
  if (!isMobile.value) nextTick(() => invalidateSize())
}

async function loadData() {
  loading.value = true
  try {
    const result = await pb.collection('locations').getFullList<Location>({ sort: 'code' })
    locations.value = result.filter(isMapped)
    renderMarkers(toMarkers(filteredLocations.value), handleMarkerClick, { fitBounds: true })
  } catch (err) {
    console.error('Failed to load map data:', err)
  } finally {
    loading.value = false
  }
}

watch(() => uiStore.theme, (t) => updateTheme(t === 'dark'))

watch(filteredLocations, (next) => {
  renderMarkers(toMarkers(next), handleMarkerClick, { fitBounds: true })
  if (selectedLocation.value && !next.some((l) => l.id === selectedLocation.value!.id)) closeDrawer()
})

onMounted(async () => {
  checkMobile()
  window.addEventListener('resize', onResize)
  await loadData()
  initMap(mapContainerId, { isDarkMode: uiStore.theme === 'dark', zoomControlPosition: 'bottomleft' })
  renderMarkers(toMarkers(filteredLocations.value), handleMarkerClick, { fitBounds: true })
})

onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  cleanup()
})
</script>

<template>
  <div
    class="h-full flex flex-col relative isolate bg-base-300 rounded-xl overflow-hidden shadow-lg border border-base-300"
    :class="props.fill ? 'min-h-0' : 'min-h-[600px]'"
  >
    <!-- Count badge (top-left) -->
    <div class="absolute top-4 left-4 z-[400]">
      <div class="badge badge-lg bg-base-100/90 backdrop-blur border-base-300 shadow-sm gap-2">
        <span>📍</span>
        <span class="font-bold">{{ filteredLocations.length }}</span>
        <span v-if="props.searchQuery && filteredLocations.length !== locations.length" class="text-xs opacity-70">
          of {{ locations.length }} mapped
        </span>
        <span v-else class="text-xs opacity-70">mapped</span>
      </div>
    </div>

    <!-- Fit-all control (top-right) -->
    <div v-if="filteredLocations.length > 0" class="absolute top-[10px] right-[10px] z-[400] flex flex-col gap-2">
      <button
        class="btn btn-sm btn-square bg-base-100 border-base-300 shadow-sm hover:bg-base-200"
        title="Fit all locations"
        @click="fitAllMarkers()"
      >
        <span class="text-lg leading-none pb-1">⊡</span>
      </button>
    </div>

    <div :id="mapContainerId" class="absolute inset-0 z-0" @click="handleMapClick"></div>

    <div v-if="loading" class="absolute inset-0 z-10 bg-base-100/50 backdrop-blur-sm flex items-center justify-center">
      <span class="loading loading-spinner loading-lg text-primary"></span>
    </div>

    <!-- No mapped locations at all -->
    <div v-if="!loading && locations.length === 0" class="absolute inset-0 z-10 flex items-center justify-center pointer-events-none">
      <div class="bg-base-100 p-6 rounded-lg shadow-xl text-center border border-base-200 pointer-events-auto max-w-sm">
        <span class="text-4xl">🗺️</span>
        <h3 class="font-bold text-lg mt-2">No mapped locations</h3>
        <p class="text-sm text-base-content/70 mt-1">Add coordinates to a location (in its editor) to place it on the map.</p>
      </div>
    </div>

    <!-- Search matched nothing -->
    <div v-else-if="!loading && filteredLocations.length === 0 && props.searchQuery" class="absolute inset-0 z-10 flex items-center justify-center pointer-events-none">
      <div class="bg-base-100 p-6 rounded-lg shadow-xl text-center border border-base-200 pointer-events-auto max-w-sm">
        <span class="text-4xl">🔍</span>
        <h3 class="font-bold text-lg mt-2">No matching locations</h3>
        <p class="text-sm text-base-content/70 mt-1">No mapped locations match your search.</p>
      </div>
    </div>

    <LocationMapDrawer
      v-if="selectedLocation"
      :location="selectedLocation"
      :is-mobile="isMobile"
      class="location-map-drawer"
      @close="closeDrawer"
    />
  </div>
</template>

<style scoped>
:deep(.leaflet-popup-content-wrapper),
:deep(.leaflet-popup-tip) {
  background-color: oklch(var(--b1));
  color: oklch(var(--bc));
  border-radius: 0.5rem;
}
:deep(.marker-selected) {
  filter: hue-rotate(180deg) saturate(1.5) drop-shadow(0 0 8px rgba(116, 128, 255, 0.8));
}
.absolute {
  pointer-events: auto;
}
</style>
