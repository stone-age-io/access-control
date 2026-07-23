<script setup lang="ts">
import { computed } from 'vue'

/**
 * Round initials avatar on a deterministic per-identity hue — hashed from `seed`
 * (fall back to the display name), so the same person is always the same colour
 * across the app. No photo support in v1: cardholders/operators carry no image
 * field, so initials are the identity.
 */
const props = withDefaults(
  defineProps<{ name?: string; seed?: string; size?: 'xs' | 'sm' | 'md' }>(),
  { name: '', seed: '', size: 'sm' },
)

const initials = computed(() => {
  const n = (props.name || '').trim()
  if (!n) return '?'
  const parts = n.split(/\s+/)
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
})

// Stable hue from a string (djb2-ish). Fixed saturation/lightness keep white text
// legible on the fill in both light and dark themes.
const hue = computed(() => {
  const s = props.seed || props.name || '?'
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360
  return h
})

const sizeClass = computed(
  () => ({ xs: 'w-6 h-6 text-[10px]', sm: 'w-8 h-8 text-xs', md: 'w-11 h-11 text-sm' })[props.size],
)
</script>

<template>
  <span
    class="inline-flex items-center justify-center rounded-full font-semibold text-white shrink-0 select-none"
    :class="sizeClass"
    :style="{ backgroundColor: `hsl(${hue} 60% 45%)` }"
    aria-hidden="true"
  >{{ initials }}</span>
</template>
