import { ref, shallowRef, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
// maplibre-gl is held at v5 on purpose. v5 inlines its tile-parsing worker into
// dist/maplibre-gl.js; v6 splits it out and resolves it as a sibling file
// (`new URL('./maplibre-gl-worker.mjs', import.meta.url)`) that Vite never
// emits once the library is bundled into a hashed chunk. Nothing throws and
// nothing reaches the console — the style still loads and its background layer
// still paints, so water, landuse, roads and labels vanish together and the map
// reads as a flat sheet of theme colour rather than as a failure. v5 is also
// the version OpenFreeMap's own quick start pins. Mirrors the platform's pin;
// revisit when @maplibre/maplibre-gl-leaflet documents v6 rather than merely
// permitting it in peerDependencies.
import 'maplibre-gl/dist/maplibre-gl.css'
// Side-effect import: registers L.maplibreGL and augments the leaflet module types.
import '@maplibre/maplibre-gl-leaflet'
import { fixLeafletIcons } from '@/utils/leafletIcons'

/**
 * Geographic Leaflet map composable for list/detail views: a themed basemap,
 * click-driven markers, and a selection highlight.
 *
 * Trimmed from the platform's version — the dashboard/KV-driven dynamic-marker
 * and clustering paths are intentionally omitted (stone-access has no dashboards
 * and a building count that doesn't need clustering).
 *
 * The basemap is fetched from a public CDN, so the geographic map needs
 * internet; the floor-plan map (useFloorPlan) does not.
 */

/**
 * Basemap styles. Keyless, and vector rather than raster.
 *
 * Both sources had to move. CARTO put its raster basemaps behind an API key and
 * is retiring them, so dark_all now serves a watermark; light mode was calling
 * tile.openstreetmap.org directly, which the OSMF tile usage policy does not
 * allow for a product. OpenFreeMap replaces both: no key, no request cap,
 * commercial use permitted, and self-hostable if a deployment ever cannot reach
 * tiles.openfreemap.org. Lifted from the platform, which moved first.
 *
 * These are MapLibre style documents, not {z}/{x}/{y} templates — OpenFreeMap
 * publishes no raster endpoint — so the basemap renders through L.maplibreGL
 * onto a WebGL canvas instead of L.tileLayer. That swap is contained here:
 * markers, selection and the floor-plan image overlay (useFloorPlan, CRS.Simple
 * with no tiles at all) are unchanged Leaflet on top of it. It does mean the
 * basemap needs WebGL.
 *
 * fiord is a designed dark style and needs no brightness correction. Do not add
 * one: the GL canvas lands in .leaflet-tile-pane, so a filter on that pane would
 * wash out the whole basemap.
 */
const STYLE_URLS = {
  light: 'https://tiles.openfreemap.org/styles/bright',
  dark: 'https://tiles.openfreemap.org/styles/fiord',
}

/**
 * OpenFreeMap's style JSON carries no `attribution` on its sources, so MapLibre
 * renders no credit of its own and ODbL still requires one. Added to the map's
 * attribution control once at init rather than per layer — it is the same for
 * both styles, and L.maplibreGL's options are typed as MapLibre's own, which
 * have no Leaflet `attribution` key.
 *
 * Three names separated by dots rather than a sentence: LocationMapViz puts the
 * zoom control at bottomleft, which is where a credit long enough to wrap grows
 * into. main.css keeps it to one line and truncates rather than wrapping.
 */
const TILE_ATTRIBUTION =
  '&copy; <a href="https://openfreemap.org">OpenFreeMap</a> &middot; <a href="https://www.openmaptiles.org/">OpenMapTiles</a> &middot; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>'

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
  const basemapLayer = shallowRef<L.MaplibreGL | null>(null)
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
      // Was an L.tileLayer option; the GL layer supplies no zoom bounds, and
      // without this Leaflet would zoom in without limit.
      maxZoom: 19,
      zoomControl: false,
      attributionControl: true,
    })

    // Leaflet's default prefix is width the credit needs, and the credit that
    // has to be there is the data's, not the library's.
    mapInstance.attributionControl.setPrefix(false)
    mapInstance.attributionControl.addAttribution(TILE_ATTRIBUTION)

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
    if (basemapLayer.value) map.value.removeLayer(basemapLayer.value)

    // attributionControl: false — the credit lives on the Leaflet control (see
    // TILE_ATTRIBUTION), not on a second one MapLibre would draw itself.
    const newLayer = L.maplibreGL({
      style: isDarkMode ? STYLE_URLS.dark : STYLE_URLS.light,
      attributionControl: false,
    })
    newLayer.addTo(map.value)
    basemapLayer.value = newLayer
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
    basemapLayer.value = null
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
