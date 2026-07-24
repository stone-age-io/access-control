<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import L from 'leaflet'
import { useFloorPlan } from '@/composables/useFloorPlan'
import { pb } from '@/utils/pb'
import type { Portal, AuxInput, AuxOutput, Location } from '@/types/pocketbase'
import {
  portalPlaceable, auxInputPlaceable, auxOutputPlaceable,
  isPlaced, parseMarkerId, PLACE_KIND_META,
  type Placeable, type PlaceKind,
} from '@/utils/placeable'
import FloorPlanDetailDrawer from '@/components/map/FloorPlanDetailDrawer.vue'
import FloorPlanPositionDrawer from '@/components/map/FloorPlanPositionDrawer.vue'

const props = defineProps<{
  location: Location
  portals: Portal[]
  auxInputs: AuxInput[]
  auxOutputs: AuxOutput[]
}>()

const emit = defineEmits<{
  'update-position': [payload: { kind: PlaceKind; id: string; position: { x: number; y: number } | null }]
}>()

const { initFloorPlan, renderMarkers, setSelected, getViewCenter } = useFloorPlan()

const loading = ref(false)
const positionMode = ref(false) // markers draggable; persists across drawer open/close
const showPositionDrawer = ref(false) // the positioning panel
const selectedId = ref<string | null>(null) // namespaced marker id (detail panel, view mode)
const isMobile = ref(false)

// Portals + aux I/O normalized into one marker list (namespaced ids so kinds
// never collide). Areas are excluded — they have no single position.
const allPlaceables = computed<Placeable[]>(() => [
  ...props.portals.map(portalPlaceable),
  ...props.auxInputs.map(auxInputPlaceable),
  ...props.auxOutputs.map(auxOutputPlaceable),
])
const placed = computed(() => allPlaceables.value.filter(isPlaced))
const unmapped = computed(() => allPlaceables.value.filter((p) => !isPlaced(p)))
const selectedPlaceable = computed(() => allPlaceables.value.find((p) => p.id === selectedId.value) || null)

// The underlying PB record for the selected marker (kind-routed).
const selectedRecord = computed<any>(() => {
  const pl = selectedPlaceable.value
  if (!pl) return null
  if (pl.kind === 'portal') return props.portals.find((r) => r.id === pl.recordId) || null
  if (pl.kind === 'aux_input') return props.auxInputs.find((r) => r.id === pl.recordId) || null
  return props.auxOutputs.find((r) => r.id === pl.recordId) || null
})

function checkMobile() {
  isMobile.value = window.innerWidth < 768
}

function escapeHtml(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] as string,
  )
}

// Editor marker: a kind emoji pin + name label, so the three kinds are
// distinguishable while arranging them (live status comes on the Live Map).
function editorIcon(item: Placeable): L.DivIcon {
  const meta = PLACE_KIND_META[item.kind]
  const label = escapeHtml(item.name || item.code)
  return L.divIcon({
    className: 'fp-marker',
    html: `<span class="fp-pin fp-pin-${item.kind}">${meta.emoji}</span><span class="fp-label">${label}</span>`,
    iconSize: [24, 24],
    iconAnchor: [12, 12],
  })
}

const loadMap = () => {
  // Reset interaction state when the floor plan changes (e.g. navigating locations).
  selectedId.value = null
  showPositionDrawer.value = false
  positionMode.value = false

  if (!props.location?.floorplan) return
  loading.value = true

  const imageUrl = pb.files.getURL(props.location, props.location.floorplan)
  const img = new Image()
  img.onload = () => {
    initFloorPlan('floorplan-container', imageUrl, img.width, img.height)
    renderAll()
    loading.value = false
  }
  img.onerror = () => {
    loading.value = false
  }
  img.src = imageUrl
}

function renderAll() {
  renderMarkers(placed.value, {
    draggable: positionMode.value,
    onMove: (markerId, x, y) => {
      const { kind, recordId } = parseMarkerId(markerId)
      emit('update-position', { kind, id: recordId, position: { x, y } })
    },
    onClick: handleMarkerClick,
    iconFor: editorIcon,
  })
  setSelected(selectedId.value)
}

// In view mode (not arranging, no panel open), clicking a marker shows its detail.
function handleMarkerClick(markerId: string) {
  if (positionMode.value || showPositionDrawer.value) return
  if (selectedId.value === markerId) {
    closeDetail()
    return
  }
  selectedId.value = markerId
  setSelected(markerId)
}

function closeDetail() {
  selectedId.value = null
  setSelected(null)
}

function togglePositionDrawer() {
  if (showPositionDrawer.value) {
    showPositionDrawer.value = false
    return
  }
  closeDetail()
  showPositionDrawer.value = true
}

function place(markerId: string) {
  const { kind, recordId } = parseMarkerId(markerId)
  emit('update-position', { kind, id: recordId, position: getViewCenter() })
  if (isMobile.value) showPositionDrawer.value = false // free the map to drag on mobile
}

function unmap(markerId: string) {
  if (selectedId.value === markerId) closeDetail()
  const { kind, recordId } = parseMarkerId(markerId)
  emit('update-position', { kind, id: recordId, position: null })
}

// Click on the map background (not a marker or drawer) closes the detail panel.
function handleMapBgClick(event: MouseEvent) {
  if (!selectedId.value) return
  const target = event.target as HTMLElement
  if (target.closest('.leaflet-marker-icon') || target.closest('.floorplan-drawer')) return
  closeDetail()
}

onMounted(() => {
  checkMobile()
  window.addEventListener('resize', checkMobile)
  loadMap()
})
onUnmounted(() => window.removeEventListener('resize', checkMobile))

watch([() => props.portals, () => props.auxInputs, () => props.auxOutputs], renderAll, { deep: true })
watch(positionMode, renderAll)
watch(() => props.location?.floorplan, loadMap)
</script>

<template>
  <div class="relative z-0 w-full h-full min-h-[500px] bg-base-300 rounded-xl overflow-hidden border border-base-300 shadow-inner">
    <div v-if="loading" class="absolute inset-0 z-20 bg-base-300/50 backdrop-blur-sm flex items-center justify-center">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <div id="floorplan-container" class="absolute inset-0 z-10" @click="handleMapBgClick"></div>

    <!-- Position toggle (top-right) -->
    <div class="absolute top-[10px] right-[10px] z-[400] flex flex-col gap-2">
      <button
        class="btn btn-sm shadow-sm gap-1"
        :class="showPositionDrawer || positionMode ? 'btn-primary' : 'bg-base-100 border-base-300 hover:bg-base-200'"
        title="Position points"
        @click="togglePositionDrawer"
      >
        🛠️ <span class="hidden sm:inline">Position</span>
      </button>
    </div>

    <!-- Detail drawer (view mode) -->
    <FloorPlanDetailDrawer
      v-if="selectedPlaceable"
      :placeable="selectedPlaceable"
      :record="selectedRecord"
      :is-mobile="isMobile"
      class="floorplan-drawer"
      @close="closeDetail"
    />

    <!-- Positioning drawer -->
    <FloorPlanPositionDrawer
      v-if="showPositionDrawer"
      :unmapped="unmapped"
      :placed="placed"
      :position-mode="positionMode"
      :is-mobile="isMobile"
      class="floorplan-drawer"
      @close="showPositionDrawer = false"
      @update:position-mode="positionMode = $event"
      @place="place"
      @unmap="unmap"
    />
  </div>
</template>

<style scoped>
:deep(.marker-selected) {
  filter: hue-rotate(180deg) saturate(1.5) drop-shadow(0 0 8px rgba(116, 128, 255, 0.8));
}
</style>

<!-- Global (un-scoped): Leaflet injects marker HTML outside the component's
     scoped DOM, so these class names must be global to take effect. -->
<style>
.leaflet-div-icon.fp-marker {
  background: transparent;
  border: 0;
  width: auto !important;
  height: auto !important;
}
.fp-pin {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 9999px;
  font-size: 13px;
  line-height: 1;
  background: oklch(var(--b1));
  border: 2px solid oklch(var(--bc) / 0.4);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.4);
}
.fp-pin-portal {
  border-color: #3b82f6;
}
.fp-pin-aux_input {
  border-color: #f59e0b;
}
.fp-pin-aux_output {
  border-color: #22c55e;
}
.fp-marker .fp-label {
  position: absolute;
  left: 26px;
  top: 2px;
  white-space: nowrap;
  font-size: 10px;
  font-weight: 600;
  color: oklch(var(--bc));
  background: oklch(var(--b1) / 0.8);
  padding: 0 4px;
  border-radius: 4px;
}
</style>
