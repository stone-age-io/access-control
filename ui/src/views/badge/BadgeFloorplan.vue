<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue'
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
 * # It fills the height it is given, and that is why it measures
 *
 * The plan used to be an `h-auto` image in a growing card, which on a phone left the bottom
 * third of the screen empty on a wide plan and pushed the action bar off it on a tall one.
 * Now the card is a flex column inside a panel of known height and the image is capped by
 * `max-h-full`/`max-w-full`, so it shrinks to fit whatever the switcher and picker leave —
 * one screen, no scrolling, the picture as big as the phone allows.
 *
 * Positions arrive as pixel coordinates in the image's own space (the same space the
 * operator's editor writes). They used to become percentages of the wrapper, which worked
 * only because the wrapper was sized BY the image. Once the image is capped in both axes it
 * is centred in a box bigger than itself on one axis, and a percentage of that box lands a
 * pin in the letterboxing — so the rendered image box is measured instead, and pins are
 * placed in px against it. `object-contain` would let CSS do the fitting but hides the
 * result: the element box stays the container's, so the same measurement problem comes back
 * with less to measure. Capping a plain image keeps element box == image box.
 *
 * A ResizeObserver on the plan area is what keeps that honest. It watches the CONTAINER, not
 * the image: selecting a pin adds the action bar below, which shortens the plan area, and on
 * a width-limited plan the image's own size does not change at all — only where it is
 * centred. Observing the image would miss exactly that case and leave every pin shifted.
 *
 * Areas are deliberately absent: only portals and aux I/O carry a position, because an
 * area is a set of points with no single place to put a pin. They live in the list.
 *
 * # A marker selects. It never acts.
 *
 * This used to be two taps on the marker itself — the first naming it, the second
 * unlocking — and it misfired, for two reasons that compounded.
 *
 * The marker was `btn btn-circle btn-xs`, which is the trap `main.css` documents: the
 * touch-target rule lifts `btn-xs` to a 2.75rem MIN-HEIGHT below 1023px and nothing else,
 * so `btn-circle`'s width won one axis and the override won the other. The hit box came out
 * 48×44 on a phone and 48×24 on a laptop — an ellipse, far larger than the dot being aimed
 * at, centred on the pin. Neighbouring pins' boxes overlapped, so a tap on one door's dot
 * could resolve to the next door's button.
 *
 * On top of that, what a tap DID depended on which pin happened to be selected: tap A, tap
 * B, tap A again and the third tap only re-selected. So a tap sometimes appeared to do
 * nothing — and worse, a double-tap aimed at one door could unlock the one whose box caught
 * it.
 *
 * Both go away by giving the marker one job. Selecting is idempotent and harmless, so an
 * overlapping hit box costs a wrong NAME on screen, which the holder can see and correct,
 * rather than a wrong door. Acting moved to a full-width labelled button below the plan —
 * the same control the Doors view uses, so there is one way to unlock a door and not two.
 * The marker is now a plain 44px button wrapping a small dot, rather than a DaisyUI `btn`
 * whose sizing classes fight each other.
 */
const props = withDefaults(
  defineProps<{
    location: BadgeLiveLocation
    enabled: boolean
    /**
     * Why the action is unavailable, when `enabled` is false. Supplied because there is
     * more than one reason: the holder's own badge is disabled by an unusable pass, while
     * the operator's read-only preview is disabled by having no session to act with — and
     * telling an operator their pass is invalid, while looking at a valid one, would send
     * them off diagnosing the wrong thing.
     */
    disabledNote?: string
  }>(),
  { disabledNote: 'Your pass is not currently valid' },
)

const natural = ref<{ w: number; h: number } | null>(null)
/** The plan area — the positioning context every pin is placed in, and what is observed. */
const area = ref<HTMLElement | null>(null)
const image = ref<HTMLImageElement | null>(null)
/**
 * The image's rendered box within the plan area, in CSS px. Null until the image has loaded
 * and been laid out, which is also what hides the pins until there is something to pin to.
 */
const box = ref<{ left: number; top: number; w: number; h: number } | null>(null)
/** The selected marker, whose name and action fill the bar under the plan. */
const selected = ref<string>('')
// One selection can act at a time, so these are scalars rather than the per-pin maps this
// component used to carry. Both are about the current selection and are cleared with it.
const busy = ref(false)
const result = ref<{ ok: boolean; message: string } | null>(null)

/**
 * Where the image actually ended up. `offsetLeft`/`offsetTop` are relative to the nearest
 * positioned ancestor, which is the plan area — so this is exactly the origin pins are
 * placed from, with no `getBoundingClientRect` and no scroll-position arithmetic.
 */
function measure() {
  const img = image.value
  if (!img || !img.offsetWidth || !img.offsetHeight) {
    box.value = null
    return
  }
  box.value = { left: img.offsetLeft, top: img.offsetTop, w: img.offsetWidth, h: img.offsetHeight }
}

let observer: ResizeObserver | null = null

function onImageLoad(e: Event) {
  const img = e.target as HTMLImageElement
  if (img.naturalWidth > 0 && img.naturalHeight > 0) {
    natural.value = { w: img.naturalWidth, h: img.naturalHeight }
  }
  measure()
  // Started on load rather than on mount: before the image has a natural size there is no
  // box to measure, so the first callbacks would have nothing to say.
  if (!observer && area.value && typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measure)
    observer.observe(area.value)
  }
}

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})

/**
 * Pixel offsets for a pin, from the image's measured box. Until the image has loaded and
 * been measured there is nothing to place against, so the pins stay hidden rather than
 * stacking at the corner.
 */
function pinStyle(p: BadgeLivePoint) {
  const b = box.value
  const n = natural.value
  if (!b || !n) return { display: 'none' }
  return {
    left: `${b.left + (p.x / n.w) * b.w}px`,
    top: `${b.top + (p.y / n.h) * b.h}px`,
  }
}

type Pin = BadgeLivePoint & { kind: 'portal' | 'output' }
const pins = computed<Pin[]>(() => [
  ...props.location.portals.map((p): Pin => ({ ...p, kind: 'portal' })),
  ...props.location.outputs.map((o): Pin => ({ ...o, kind: 'output' })),
])

const selectedPin = computed<Pin | undefined>(() => pins.value.find((p) => p.id === selected.value))
const actionLabel = computed(() =>
  selectedPin.value?.kind === 'portal' ? 'Unlock this door' : 'Activate this control',
)

function select(pin: Pin) {
  if (selected.value === pin.id) return
  selected.value = pin.id
  // The outcome belonged to the previous selection; carrying it over would caption one door
  // with another's result.
  result.value = null
}

async function act() {
  const pin = selectedPin.value
  if (!pin || !pin.remote || !props.enabled || busy.value) return

  busy.value = true
  result.value = null
  const path =
    pin.kind === 'portal' ? `/api/badge/unlock/${pin.id}` : `/api/badge/outputs/${pin.id}/pulse`
  try {
    await badgePb.send(path, { method: 'POST' })
    result.value = { ok: true, message: pin.kind === 'portal' ? 'Unlocked' : 'Activated' }
  } catch (err: any) {
    result.value = { ok: false, message: actionErrorText(err) }
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="card bg-base-100 shadow-sm h-full">
    <!-- A flex column: the title and the action bar take what they need, the plan area takes
         the rest. `min-h-0` on both this and the plan area is what allows that rest to be
         SMALLER than the image's intrinsic height — without it the column floors at the
         content and the card grows past the viewport again. -->
    <div class="card-body flex min-h-0 flex-col gap-3 p-3">
      <h2 class="card-title shrink-0 text-base px-1">{{ location.name }}</h2>

      <!-- The plan. `relative` is the positioning context every pin is measured and placed
           against; the flex centring is what letterboxes a plan whose shape does not match
           the space, and the image caps itself rather than stretching to fill. -->
      <div
        ref="area"
        class="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden rounded-lg bg-base-200"
      >
        <img
          ref="image"
          :src="location.floorplan"
          :alt="`Floor plan of ${location.name}`"
          class="block h-auto max-h-full w-auto max-w-full select-none"
          @load="onImageLoad"
        />
        <!-- A 44px square hit area wrapping a small visible dot: big enough to hit on a
             phone, small enough not to cover the plan it annotates. The dot is
             pointer-events-none so a tap always resolves to the button, never the span. -->
        <button
          v-for="pin in pins"
          :key="pin.id"
          type="button"
          class="absolute grid h-11 w-11 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary"
          :style="pinStyle(pin)"
          :title="pin.name"
          :aria-label="pin.name"
          :aria-pressed="pin.id === selected"
          @click="select(pin)"
        >
          <span
            class="pointer-events-none flex h-7 w-7 items-center justify-center rounded-full border text-xs shadow transition-colors"
            :class="
              pin.id === selected
                ? 'bg-primary text-primary-content border-primary scale-110'
                : pin.remote
                  ? 'bg-base-100 border-base-content/30'
                  : 'bg-base-100 border-base-content/15 opacity-70'
            "
          >
            {{ pin.kind === 'portal' ? '🚪' : '⚡' }}
          </span>
        </button>
      </div>

      <!-- One caption + action bar for whichever pin is selected, rather than a label per
           pin: on a phone-sized plan, per-pin text would cover the plan it annotates. -->
      <div v-if="selectedPin" class="shrink-0 px-1 space-y-2">
        <div class="text-sm font-medium">{{ selectedPin.name }}</div>

        <button
          v-if="selectedPin.remote"
          type="button"
          class="btn btn-primary w-full justify-between"
          :disabled="busy || !enabled"
          @click="act"
        >
          <span>{{ actionLabel }}</span>
          <span v-if="busy" class="loading loading-spinner loading-sm"></span>
        </button>
        <p v-else class="text-xs text-base-content/60">
          Use this one in person — it cannot be opened remotely.
        </p>

        <p v-if="selectedPin.remote && !enabled" class="text-xs text-base-content/60">
          {{ disabledNote }}
        </p>
        <p v-if="result" class="text-xs" :class="result.ok ? 'text-success' : 'text-error'">
          {{ result.message }}
        </p>
      </div>
      <p v-else class="shrink-0 text-xs text-base-content/50 px-1">
        Tap a marker to see which door it is.
      </p>
    </div>
  </div>
</template>
