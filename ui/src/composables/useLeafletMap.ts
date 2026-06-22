import { ref, shallowRef, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { fixLeafletIcons } from '@/utils/leafletIcons'

/**
 * Geographic Leaflet map composable for list/detail views: themed OSM/CARTO
 * tiles, click-driven markers, and a selection highlight.
 *
 * Trimmed from the platform's version — the dashboard/KV-driven dynamic-marker
 * and clustering paths are intentionally omitted (stone-access has no dashboards
 * and a building count that doesn't need clustering), which also keeps the
 * dependency surface to plain `leaflet`.
 *
 * Tiles are fetched from public CDNs (OpenStreetMap / CARTO), so the geographic
 * map needs internet; the floor-plan map (useFloorPlan) does not.
 */

const TILE_URLS = {
  light: 'https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png',
  dark: 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png',
}
const TILE_ATTRIBUTION =
  '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>'

const DEFAULT_CENTER = { lat: 39.8283, lon: -98.5795 }
const DEFAULT_ZOOM = 4

export interface MapMarkerInput {
  id: string
  lat: number
  lon: number
  label?: string
}

export type ZoomControlPosition = 'topleft' | 'topright' | 'bottomleft' | 'bottomright' | 'none'

export interface InitMapOptions {
  isDarkMode: boolean
  center?: { lat: number; lon: number }
  zoom?: number
  zoomControlPosition?: ZoomControlPosition
}

export interface FitOptions {
  padding?: number
  maxZoom?: number
}

export function useLeafletMap() {
  const map = shallowRef<L.Map | null>(null)
  const markersLayer = shallowRef<L.LayerGroup | null>(null)
  const tileLayer = shallowRef<L.TileLayer | null>(null)
  const initialized = ref(false)

  const markerInstances = new Map<string, L.Marker>()
  let selectedMarkerId: string | null = null

  function initMap(containerId: string, opts: InitMapOptions) {
    if (map.value) {
      map.value.remove()
      map.value = null
    }
    markerInstances.clear()
    selectedMarkerId = null

    fixLeafletIcons()

    const container = document.getElementById(containerId)
    if (!container) return
    if ((container as any)._leaflet_id) (container as any)._leaflet_id = null

    const center = opts.center ?? DEFAULT_CENTER
    const zoom = opts.zoom ?? DEFAULT_ZOOM
    const zoomControlPosition = opts.zoomControlPosition ?? 'topleft'

    const mapInstance = L.map(containerId, {
      center: [center.lat, center.lon],
      zoom,
      zoomControl: false,
      attributionControl: true,
    })

    if (zoomControlPosition !== 'none') {
      L.control.zoom({ position: zoomControlPosition }).addTo(mapInstance)
    }

    map.value = mapInstance
    updateTheme(opts.isDarkMode)

    const layerGroup = L.layerGroup()
    layerGroup.addTo(mapInstance)
    markersLayer.value = layerGroup

    initialized.value = true
    setTimeout(() => mapInstance.invalidateSize(), 100)
  }

  function updateTheme(isDarkMode: boolean) {
    if (!map.value) return
    if (tileLayer.value) map.value.removeLayer(tileLayer.value)

    const url = isDarkMode ? TILE_URLS.dark : TILE_URLS.light
    const newTileLayer = L.tileLayer(url, { attribution: TILE_ATTRIBUTION, maxZoom: 19 })
    newTileLayer.addTo(map.value)
    tileLayer.value = newTileLayer
  }

  function renderMarkers(
    markers: MapMarkerInput[],
    onMarkerClick?: (id: string) => void,
    opts: { fitBounds?: boolean } = {},
  ) {
    if (!map.value || !markersLayer.value) return

    markerInstances.forEach((marker) => markersLayer.value!.removeLayer(marker))
    markerInstances.clear()

    markers.forEach(({ id, lat, lon, label }) => {
      const marker = L.marker([lat, lon], { title: label })
      ;(marker as any).__rrId = id

      if (onMarkerClick) marker.on('click', () => onMarkerClick(id))
      if (label) {
        marker.bindTooltip(label, { permanent: false, direction: 'top', offset: [-15, -15] })
      }

      markerInstances.set(id, marker)
      markersLayer.value!.addLayer(marker)
    })

    if (selectedMarkerId) applySelectionClass(selectedMarkerId)
    if (opts.fitBounds) fitAllMarkers()
  }

  function applySelectionClass(markerId: string | null) {
    markerInstances.forEach((marker) => marker.getElement()?.classList.remove('marker-selected'))
    if (markerId) markerInstances.get(markerId)?.getElement()?.classList.add('marker-selected')
  }

  function setSelectedMarker(markerId: string | null) {
    selectedMarkerId = markerId
    applySelectionClass(markerId)
  }

  function fitAllMarkers(opts: FitOptions = {}): boolean {
    if (!map.value || markerInstances.size === 0) return false

    const bounds = L.latLngBounds([])
    markerInstances.forEach((m) => bounds.extend(m.getLatLng()))
    if (!bounds.isValid()) return false

    const padding = opts.padding ?? 50
    const maxZoom = opts.maxZoom ?? 15
    map.value.fitBounds(bounds, { padding: [padding, padding], maxZoom })
    return true
  }

  function invalidateSize() {
    map.value?.invalidateSize()
  }

  function cleanup() {
    if (map.value) {
      map.value.remove()
      map.value = null
    }
    markersLayer.value = null
    tileLayer.value = null
    markerInstances.clear()
    selectedMarkerId = null
    initialized.value = false
  }

  onUnmounted(cleanup)

  return {
    map,
    initialized,
    initMap,
    updateTheme,
    renderMarkers,
    setSelectedMarker,
    fitAllMarkers,
    invalidateSize,
    cleanup,
  }
}
