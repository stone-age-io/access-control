import { ref, shallowRef, onUnmounted } from 'vue'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import type { Portal } from '@/types/pocketbase'
import { fixLeafletIcons } from '@/utils/leafletIcons'

export interface RenderOptions {
  draggable: boolean
  onMove: (id: string, x: number, y: number) => void
  onClick: (id: string) => void
  /** Optional per-portal marker icon (e.g. status-colored divIcon on the
   *  operational map). When omitted, Leaflet's default pin is used. */
  iconFor?: (portal: Portal) => L.Icon | L.DivIcon
}

/**
 * Floor-plan map composable: a Leaflet map in CRS.Simple (pixel) space with an
 * image overlay. Mirrors the marker bookkeeping/selection of useLeafletMap, but
 * kept separate because it renders an uploaded image instead of geographic tiles
 * — and so needs no internet.
 *
 * Ported from the platform's useFloorPlan; the only change is Thing → Portal.
 */
export function useFloorPlan() {
  const map = shallowRef<L.Map | null>(null)
  const markerLayer = shallowRef<L.LayerGroup | null>(null)
  const mapInitialized = ref(false)

  const markerInstances = new Map<string, L.Marker>()
  let selectedId: string | null = null

  // Detach handlers and drop all markers — used on re-render, re-init, and cleanup.
  // off() releases the click/dragend closures (which capture the parent's emit).
  const clearMarkers = () => {
    markerInstances.forEach((m) => m.off())
    markerLayer.value?.clearLayers()
    markerInstances.clear()
  }

  const initFloorPlan = (containerId: string, imageUrl: string, width: number, height: number) => {
    clearMarkers()
    if (map.value) map.value.remove()
    selectedId = null

    fixLeafletIcons() // ensure default marker icons resolve (broken on iOS otherwise)

    const container = document.getElementById(containerId)
    if (!container) return
    if ((container as any)._leaflet_id) (container as any)._leaflet_id = null

    map.value = L.map(containerId, {
      crs: L.CRS.Simple, // pixel coordinate space (1 unit = 1 image pixel)
      minZoom: -2,
      maxZoom: 2,
      attributionControl: false,
    })

    const imageBounds = L.latLngBounds(L.latLng(0, 0), L.latLng(height, width))
    L.imageOverlay(imageUrl, imageBounds).addTo(map.value)
    markerLayer.value = L.layerGroup().addTo(map.value)
    map.value.fitBounds(imageBounds)
    mapInitialized.value = true
  }

  // Renders only placed portals (those with a floorplan_position). Unplaced
  // portals live in the positioning drawer until explicitly placed.
  const renderMarkers = (portals: Portal[], opts: RenderOptions) => {
    if (!markerLayer.value || !map.value) return
    clearMarkers()

    portals.forEach((portal) => {
      const pos = portal.floorplan_position
      if (!pos || typeof pos.x !== 'number' || typeof pos.y !== 'number') return

      // Leaflet uses [lat, lng] = [y, x] in CRS.Simple.
      const icon = opts.iconFor?.(portal)
      const marker = L.marker([pos.y, pos.x], {
        draggable: opts.draggable,
        title: portal.name || portal.code,
        ...(icon ? { icon } : {}),
      })

      marker.on('click', () => opts.onClick(portal.id))
      if (opts.draggable) {
        marker.on('dragend', (e) => {
          const { lat, lng } = (e.target as L.Marker).getLatLng()
          opts.onMove(portal.id, Math.round(lng), Math.round(lat))
        })
      }

      markerInstances.set(portal.id, marker)
      marker.addTo(markerLayer.value!)
    })

    applySelection(selectedId)
  }

  const applySelection = (id: string | null) => {
    markerInstances.forEach((m) => m.getElement()?.classList.remove('marker-selected'))
    if (id) markerInstances.get(id)?.getElement()?.classList.add('marker-selected')
  }

  const setSelected = (id: string | null) => {
    selectedId = id
    applySelection(id)
  }

  // Current viewport center as floor-plan pixel coords — where a newly placed marker drops.
  const getViewCenter = (): { x: number; y: number } => {
    const c = map.value?.getCenter()
    return { x: Math.round(c?.lng ?? 0), y: Math.round(c?.lat ?? 0) }
  }

  const invalidateSize = () => map.value?.invalidateSize()

  const cleanup = () => {
    clearMarkers()
    if (map.value) {
      map.value.remove()
      map.value = null
    }
    markerLayer.value = null
    selectedId = null
    mapInitialized.value = false
  }

  onUnmounted(cleanup)

  return { map, mapInitialized, initFloorPlan, renderMarkers, setSelected, getViewCenter, invalidateSize, cleanup }
}
