<script setup lang="ts">
import { useUIStore } from '@/stores/ui'

/**
 * Light/dark toggle. One component so every surface that offers it behaves the same.
 *
 * It exists as a shared component mainly because of where it is NEEDED: the operator
 * console has a sidebar and a mobile header to hang it off, but the badge tier and the
 * sign-in page have no chrome at all, and a badge holder on a phone in a dark stairwell
 * has the strongest claim on it of anyone.
 *
 * # One size, and why there is no `size` prop
 *
 * This is `.btn .btn-square`, which DaisyUI makes 3rem — a 48px square, comfortably past
 * the 44px floor for a touch target, at every breakpoint. It briefly took a `size` prop
 * whose `sm` value was the bug: `btn-square btn-sm` computes to 48 WIDE by 32 TALL,
 * because DaisyUI emits `.btn-sm{height:2rem}` after `.btn-square{height:3rem}` and the
 * later rule wins. main.css lifts `btn-sm`'s min-height to 2.75rem, but only below
 * 1023px — so the same control was 44px tall on a phone and 32px on a laptop, which is
 * exactly the "have to land it just right" feeling that rule exists to prevent.
 *
 * The store owns the persistence (localStorage + the `data-theme` attribute), so this is
 * only the affordance.
 */
const uiStore = useUIStore()
</script>

<template>
  <button
    type="button"
    class="btn btn-square btn-ghost"
    :title="uiStore.theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
    aria-label="Toggle light/dark theme"
    @click="uiStore.toggleTheme"
  >
    <!-- Emoji rather than an icon set: this is the one control that must render before
         any theme decision has been made, and it matches the existing header button. -->
    <span class="text-xl">{{ uiStore.theme === 'dark' ? '☀️' : '🌙' }}</span>
  </button>
</template>
