<script setup lang="ts">
import { ref, computed } from 'vue'
import { actionErrorText } from './reasonText'
import { badgePb } from '@/utils/badgePb'
import type { BadgeLiveLocation, BadgeLivePoint } from '@/types/badge'

/**
 * The holder's own doors and controls pinned on a site's floor plan.
 *
 * # Why this is an <img> and not the operator's Leaflet map
 *
 * The operator's Live View is a commissioning and monitoring surface: pan, zoom, drag to
 * place, live status on every point in the building. That needs Leaflet, which is ~150 kB
 * of the operator bundle. A badge shows a handful of doors on a phone, where the whole
 * plan fits the screen width and the useful gesture is "tap the one I'm standing at" —
 * so this is an image with absolutely-positioned buttons, and the phone downloads no map
 * library at all.
 *
 * Positions arrive as pixel coordinates in the image's own space (the same space the
 * operator's editor writes). They become percentages once the image reports its natural
 * size, so the plan scales to any screen without the server knowing anything about the
 * image.
 *
 * Areas are deliberately absent: only portals and aux I/O carry a position, because an
 * area is a set of points with no single place to put a pin. They live in the list.
 */
const props = withDefaults(
  defineProps<{
    location: BadgeLiveLocation
    enabled: boolean
    /**
     * Why a tap does nothing, when `enabled` is false. Supplied because there is more than
     * one reason: the holder's own badge is disabled by an unusable pass, while the
     * operator's read-only preview is disabled by having no session to act with — and
     * telling an operator their pass is invalid, while looking at a valid one, would send
     * them off diagnosing the wrong thing.
     */
    disabledNote?: string
  }>(),
  { disabledNote: 'Your pass is not currently valid' },
)

const natural = ref<{ w: number; h: number } | null>(null)
const busy = ref<Record<string, boolean>>({})
const results = ref<Record<string, { ok: boolean; message: string }>>({})
/** The last-tapped pin, so the outcome message has somewhere to appear. */
const selected = ref<string>('')

function onImageLoad(e: Event) {
  const img = e.target as HTMLImageElement
  if (img.naturalWidth > 0 && img.naturalHeight > 0) {
    natural.value = { w: img.naturalWidth, h: img.naturalHeight }
  }
}

/** Percent offsets for a pin. Until the image has loaded there is nothing to place against. */
function pinStyle(p: BadgeLivePoint) {
  const n = natural.value
  if (!n) return { display: 'none' }
  return {
    left: `${(p.x / n.w) * 100}%`,
    top: `${(p.y / n.h) * 100}%`,
  }
}

type Pin = BadgeLivePoint & { kind: 'portal' | 'output' }
const pins = computed<Pin[]>(() => [
  ...props.location.portals.map((p): Pin => ({ ...p, kind: 'portal' })),
  ...props.location.outputs.map((o): Pin => ({ ...o, kind: 'output' })),
])

const selectedResult = computed(() => (selected.value ? results.value[selected.value] : undefined))
const selectedPin = computed(() => pins.value.find((p) => p.id === selected.value))

function note(id: string, ok: boolean, message: string) {
  results.value = { ...results.value, [id]: { ok, message } }
}

/**
 * Two taps to act, not one.
 *
 * The first tap on a marker only names it — which is the more common thing a holder wants
 * ("which door is this?") and, on a phone-sized plan, the only safe default: these
 * markers are a few millimetres across, and a single tap firing a real unlock would make
 * a mis-touch open a door. The second tap on an already-selected marker is the deliberate
 * one. The button list on the Access tab needs no such guard, because those targets are
 * full-width and labelled.
 */
async function tap(pin: Pin) {
  if (selected.value !== pin.id) {
    selected.value = pin.id
    delete results.value[pin.id]
    // A marker the holder cannot drive remotely is still worth tapping — it says which
    // door they are looking at. Better than a button that silently does nothing.
    if (!pin.remote) note(pin.id, false, 'Use this one in person')
    return
  }

  if (!pin.remote) return // already said so; a second tap changes nothing
  if (!props.enabled) {
    note(pin.id, false, props.disabledNote)
    return
  }

  busy.value = { ...busy.value, [pin.id]: true }
  delete results.value[pin.id]
  const path =
    pin.kind === 'portal' ? `/api/badge/unlock/${pin.id}` : `/api/badge/outputs/${pin.id}/pulse`
  try {
    await badgePb.send(path, { method: 'POST' })
    note(pin.id, true, pin.kind === 'portal' ? 'Unlocked' : 'Activated')
  } catch (err: any) {
    note(pin.id, false, actionErrorText(err))
  } finally {
    const next = { ...busy.value }
    delete next[pin.id]
    busy.value = next
  }
}
</script>

<template>
  <div class="card bg-base-100 shadow-sm">
    <div class="card-body gap-3 p-3">
      <h2 class="card-title text-base px-1">{{ location.name }}</h2>

      <!-- The plan. `relative` is the positioning context every pin is placed against, so
           the image and the markers scale together with no JS on resize. -->
      <div class="relative bg-base-200 rounded-lg overflow-hidden">
        <img
          :src="location.floorplan"
          :alt="`Floor plan of ${location.name}`"
          class="block w-full h-auto select-none"
          @load="onImageLoad"
        />
        <button
          v-for="pin in pins"
          :key="pin.id"
          type="button"
          class="absolute -translate-x-1/2 -translate-y-1/2 btn btn-circle btn-xs shadow"
          :class="[
            pin.id === selected ? 'btn-primary' : pin.remote ? 'btn-neutral' : 'btn-ghost bg-base-100',
            busy[pin.id] ? 'loading' : '',
          ]"
          :style="pinStyle(pin)"
          :title="pin.name"
          :aria-label="pin.name"
          @click="tap(pin)"
        >
          <span v-if="!busy[pin.id]">{{ pin.kind === 'portal' ? '🚪' : '⚡' }}</span>
        </button>
      </div>

      <!-- One caption area for whichever pin was last tapped, rather than a label per pin:
           on a phone-sized plan, per-pin text would cover the plan it annotates. -->
      <div v-if="selectedPin" class="px-1 space-y-1">
        <div class="text-sm font-medium">{{ selectedPin.name }}</div>
        <p
          v-if="selectedResult"
          class="text-xs"
          :class="selectedResult.ok ? 'text-success' : 'text-error'"
        >
          {{ selectedResult.message }}
        </p>
        <p v-else-if="selectedPin.remote" class="text-xs text-base-content/60">
          Tap again to {{ selectedPin.kind === 'portal' ? 'unlock' : 'activate' }}.
        </p>
      </div>
      <p v-else class="text-xs text-base-content/50 px-1">
        Tap a marker to open it, or to see which one it is.
      </p>
    </div>
  </div>
</template>
