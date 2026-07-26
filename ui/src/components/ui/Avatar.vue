<script setup lang="ts">
import { computed, ref, watch } from 'vue'

/**
 * Identity avatar: the person's photo when there is one, else round initials on a
 * deterministic per-identity hue — hashed from `seed` (falling back to the display
 * name), so the same person is always the same colour across the app.
 *
 * `src` is resolved by the CALLER, not here, because `cardholders.photo` is a
 * protected file and its URL needs a session file token (see useFileUrl). Keeping
 * the token out of this component leaves it dumb and usable for operators, who have
 * no photo field at all.
 *
 * A photo that fails to load falls back to initials rather than a broken image —
 * relevant because a file token expires, so a long-open page can outlive its URLs.
 */
const props = withDefaults(
  defineProps<{
    name?: string
    seed?: string
    size?: 'xs' | 'sm' | 'md' | 'lg'
    src?: string
  }>(),
  { name: '', seed: '', size: 'sm', src: '' },
)

const failed = ref(false)
// A new URL deserves a fresh attempt — e.g. the token arrived, or the record changed.
watch(
  () => props.src,
  () => {
    failed.value = false
  },
)

const showPhoto = computed(() => !!props.src && !failed.value)

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
  () =>
    ({
      xs: 'w-6 h-6 text-[10px]',
      sm: 'w-8 h-8 text-xs',
      md: 'w-11 h-11 text-sm',
      lg: 'w-24 h-24 text-2xl',
    })[props.size],
)
</script>

<template>
  <img
    v-if="showPhoto"
    :src="src"
    :alt="name || 'cardholder photo'"
    class="inline-block rounded-full object-cover shrink-0 select-none bg-base-300"
    :class="sizeClass"
    @error="failed = true"
  />
  <span
    v-else
    class="inline-flex items-center justify-center rounded-full font-semibold text-white shrink-0 select-none"
    :class="sizeClass"
    :style="{ backgroundColor: `hsl(${hue} 60% 45%)` }"
    aria-hidden="true"
    >{{ initials }}</span
  >
</template>
