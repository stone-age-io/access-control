<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import L from 'leaflet'
import { useUIStore } from '@/stores/ui'
import { useLeafletMap } from '@/composables/useLeafletMap'
import { useToast } from '@/composables/useToast'

/**
 * Address-search + map picker for a location's coordinates. Two-way binds the
 * `lat`/`lon` v-models so the parent form's numeric inputs stay the source of
 * truth and the map merely visualizes/edits them.
 *
 * Three ways to set the pin: search a place (explicit — on Enter or the button,
 * never debounced, to respect Nominatim's usage policy), click the map, or drag
 * the pin. The search hits Nominatim's public endpoint directly from the browser
 * (same OSM ecosystem as the tiles in useLeafletMap), so it needs internet and
 * degrades to manual lat/lon entry when offline.
 */

const lat = defineModel<number>('lat', { required: true })
const lon = defineModel<number>('lon', { required: true })

const uiStore = useUIStore()
const toast = useToast()
const { map, initMap, updateTheme, invalidateSize } = useLeafletMap()

const containerId = 'location-picker-map-container'

interface NominatimResult {
  display_name: string
  lat: string
  lon: string
}

const searchQuery = ref('')
const searching = ref(false)
const results = ref<NominatimResult[]>([])

let marker: L.Marker | null = null
// Recenter the map at most once (on the first non-zero coordinates, e.g. an
// edit-mode record load) — never on every manual keystroke.
let centered = false

const hasCoords = computed(() => (lat.value ?? 0) !== 0 || (lon.value ?? 0) !== 0)

function placeMarker(la: number, lo: number) {
  if (!map.value) return
  if (!marker) {
    marker = L.marker([la, lo], { draggable: true })
    marker.on('dragend', () => {
      const ll = marker!.getLatLng()
      setCoords(ll.lat, ll.lng)
    })
    marker.addTo(map.value)
  } else {
    marker.setLatLng([la, lo])
  }
}

// Round to ~0.1m precision so the stored value matches what we hand the map
// (keeps syncMarkerFromModel's epsilon check exact, avoiding a redundant move).
function round6(n: number): number {
  return Math.round(n * 1e6) / 1e6
}

// Write back to the models.
function setCoords(la: number, lo: number) {
  lat.value = round6(la)
  lon.value = round6(lo)
}

// Reflect external model changes (record load, manual numeric input, clear) onto
// the map. Our own writes land here too but no-op via the epsilon check.
function syncMarkerFromModel() {
  if (!map.value) return
  if (!hasCoords.value) {
    if (marker) {
      map.value.removeLayer(marker)
      marker = null
    }
    centered = false
    return
  }
  const la = lat.value
  const lo = lon.value
  if (marker) {
    const ll = marker.getLatLng()
    if (Math.abs(ll.lat - la) < 1e-7 && Math.abs(ll.lng - lo) < 1e-7) return
  }
  placeMarker(la, lo)
  if (!centered) {
    map.value.setView([la, lo], 17)
    centered = true
  }
}

async function search() {
  const q = searchQuery.value.trim()
  if (!q || searching.value) return
  searching.value = true
  results.value = []
  try {
    const url = `https://nominatim.openstreetmap.org/search?format=json&limit=5&q=${encodeURIComponent(q)}`
    const resp = await fetch(url, { headers: { Accept: 'application/json' } })
    if (!resp.ok) throw new Error(`Search failed (${resp.status})`)
    results.value = (await resp.json()) as NominatimResult[]
    if (results.value.length === 0) toast.info('No matching places found')
  } catch (err: any) {
    toast.error(err?.message || 'Address search failed (needs internet)')
  } finally {
    searching.value = false
  }
}

function selectResult(r: NominatimResult) {
  const la = round6(parseFloat(r.lat))
  const lo = round6(parseFloat(r.lon))
  if (Number.isNaN(la) || Number.isNaN(lo)) return
  setCoords(la, lo)
  // Drive the map from the parsed values directly — reading lat/lon back here
  // returns the *previous* value, since the parent v-model prop hasn't re-flowed
  // down yet (the watch reconciles on the next tick, when it has).
  placeMarker(la, lo)
  map.value?.setView([la, lo], 17)
  centered = true
  results.value = []
  searchQuery.value = r.display_name
}

function clearPin() {
  setCoords(0, 0)
}

onMounted(() => {
  const center = hasCoords.value ? { lat: lat.value, lon: lon.value } : undefined
  const zoom = hasCoords.value ? 17 : undefined
  initMap(containerId, {
    isDarkMode: uiStore.theme === 'dark',
    center,
    zoom,
    zoomControlPosition: 'topright',
  })
  centered = hasCoords.value
  if (hasCoords.value) placeMarker(lat.value, lon.value)
  map.value?.on('click', (e: L.LeafletMouseEvent) => setCoords(e.latlng.lat, e.latlng.lng))
  nextTick(() => invalidateSize())
})

watch([lat, lon], syncMarkerFromModel)
watch(() => uiStore.theme, (t) => updateTheme(t === 'dark'))
</script>

<template>
  <div class="space-y-2">
    <!-- Address search (explicit: Enter or the button) -->
    <div class="relative">
      <div class="flex gap-2">
        <input
          v-model="searchQuery"
          type="text"
          placeholder="Search an address or place…"
          class="input input-bordered flex-1"
          @keydown.enter.prevent="search"
        />
        <button type="button" class="btn btn-primary" :disabled="searching" @click="search">
          <span v-if="searching" class="loading loading-spinner loading-sm"></span>
          <span v-else>Search</span>
        </button>
      </div>
      <ul
        v-if="results.length"
        class="absolute z-[600] mt-1 w-full bg-base-100 border border-base-300 rounded-box shadow-lg max-h-60 overflow-y-auto"
      >
        <li v-for="(r, i) in results" :key="i">
          <button
            type="button"
            class="w-full text-left px-3 py-2 text-sm hover:bg-base-200 transition-colors"
            @click="selectResult(r)"
          >
            {{ r.display_name }}
          </button>
        </li>
      </ul>
    </div>

    <!-- Map -->
    <div class="relative h-72 rounded-lg overflow-hidden border border-base-300">
      <div :id="containerId" class="absolute inset-0 z-0"></div>
      <button
        v-if="hasCoords"
        type="button"
        class="btn btn-xs absolute top-2 left-2 z-[400] bg-base-100/90 backdrop-blur border-base-300 shadow-sm hover:bg-base-200"
        @click="clearPin"
      >
        Clear pin
      </button>
    </div>

    <p class="text-xs leading-relaxed text-base-content/60">
      Search for a place, click the map, or drag the pin to set coordinates. Geocoding by
      <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener" class="link">OpenStreetMap / Nominatim</a>
      (needs internet).
    </p>
  </div>
</template>
