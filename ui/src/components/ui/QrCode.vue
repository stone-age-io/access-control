<script setup lang="ts">
import { computed } from 'vue'
import qrcode from 'qrcode-generator'

/**
 * Renders a payload as an inline SVG QR code.
 *
 * The SVG is built from the module matrix rather than the library's
 * `createSvgTag()` so the colours are ours: the dark modules use `currentColor` and
 * the quiet zone is a plain light plate. A QR code MUST keep high contrast and a
 * light background to scan reliably, so the plate stays white even in dark mode —
 * inverting it would break cheap scanners. `currentColor` lets a caller tint the
 * modules if it ever wants to.
 *
 * Mode is chosen per payload: QR's alphanumeric mode (digits, A-Z, and a few
 * symbols) packs far more densely than byte mode, so an uppercase base32 visitor
 * credential produces a noticeably smaller, easier-to-scan code. A lowercase
 * payload — e.g. a PocketBase cardholder id — falls back to byte mode.
 */
const props = withDefaults(
  defineProps<{
    /** Payload to encode. Empty renders nothing. */
    value: string
    /** Rendered edge length in CSS pixels. */
    size?: number
    /**
     * Error-correction level. 'M' (~15% recovery) is the sensible default for a
     * screen; a phone display is not a scuffed printed card needing 'H'.
     */
    level?: 'L' | 'M' | 'Q' | 'H'
  }>(),
  { size: 220, level: 'M' },
)

// QR alphanumeric charset: 0-9 A-Z space $ % * + - . / :
const ALNUM = /^[0-9A-Z $%*+\-./:]+$/

type Matrix = { count: number; dark: boolean[][] } | null

const matrix = computed<Matrix>(() => {
  const value = props.value
  if (!value) return null
  try {
    // typeNumber 0 = pick the smallest version that fits.
    const qr = qrcode(0, props.level)
    qr.addData(value, ALNUM.test(value) ? 'Alphanumeric' : 'Byte')
    qr.make()
    const count = qr.getModuleCount()
    const dark: boolean[][] = []
    for (let r = 0; r < count; r++) {
      const row: boolean[] = []
      for (let c = 0; c < count; c++) row.push(qr.isDark(r, c))
      dark.push(row)
    }
    return { count, dark }
  } catch {
    // Payload too long for any version, or an encoding refusal. Render nothing
    // rather than a corrupt code that looks scannable but is not.
    return null
  }
})

/** Quiet zone in modules. The spec calls for 4; anything less hurts scan rates. */
const MARGIN = 4

const viewBox = computed(() => {
  const m = matrix.value
  if (!m) return '0 0 1 1'
  const side = m.count + MARGIN * 2
  return `0 0 ${side} ${side}`
})

/**
 * One path for every dark module, as a run-length-merged set of horizontal bars.
 * Merging runs keeps the DOM small (a version-4 code is 33x33 = 1089 modules, and
 * one <rect> each is needlessly heavy for something that never animates).
 */
const path = computed(() => {
  const m = matrix.value
  if (!m) return ''
  const parts: string[] = []
  for (let r = 0; r < m.count; r++) {
    let c = 0
    while (c < m.count) {
      if (!m.dark[r][c]) {
        c++
        continue
      }
      let run = 1
      while (c + run < m.count && m.dark[r][c + run]) run++
      parts.push(`M${c + MARGIN} ${r + MARGIN}h${run}v1h-${run}z`)
      c += run
    }
  }
  return parts.join('')
})
</script>

<template>
  <svg
    v-if="matrix"
    :viewBox="viewBox"
    :width="size"
    :height="size"
    shape-rendering="crispEdges"
    role="img"
    aria-label="QR code"
    class="rounded-lg"
  >
    <!-- Light plate incl. quiet zone: required for reliable scanning, so it is
         intentionally NOT theme-dependent. -->
    <rect width="100%" height="100%" fill="#ffffff" />
    <path :d="path" fill="#000000" />
  </svg>
</template>
