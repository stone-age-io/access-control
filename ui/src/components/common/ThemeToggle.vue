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
 * The store owns the persistence (localStorage + the `data-theme` attribute), so this is
 * only the affordance.
 */
const uiStore = useUIStore()

const props = withDefaults(defineProps<{ size?: 'xs' | 'sm' | 'md' }>(), { size: 'md' })

// A lookup, not `btn-${size}`: Tailwind's scanner only sees literal class names, so an
// interpolated one survives only as long as some other file happens to use it.
const BTN_SIZE = { xs: 'btn-xs', sm: 'btn-sm', md: '' } as const
const ICON_SIZE = { xs: 'text-base', sm: 'text-lg', md: 'text-xl' } as const
</script>

<template>
  <button
    type="button"
    class="btn btn-square btn-ghost"
    :class="BTN_SIZE[props.size]"
    :title="uiStore.theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
    aria-label="Toggle light/dark theme"
    @click="uiStore.toggleTheme"
  >
    <!-- Emoji rather than an icon set: this is the one control that must render before
         any theme decision has been made, and it matches the existing header button. -->
    <span :class="ICON_SIZE[props.size]">{{ uiStore.theme === 'dark' ? '☀️' : '🌙' }}</span>
  </button>
</template>
