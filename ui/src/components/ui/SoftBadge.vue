<script setup lang="ts">
import type { SoftTone } from '@/utils/badges'

/**
 * The app-wide status/type chip (see .badge-soft in assets/main.css). `tone`
 * selects the semantic tint; `dot` adds a leading state dot — use it for status
 * (active/armed/online/allow/…) and omit it for plain type/tag/count chips. Extra
 * utility classes fall through onto the root span, so `<SoftBadge class="font-mono
 * ml-2">` composes as expected.
 */
withDefaults(defineProps<{ tone?: SoftTone; dot?: boolean }>(), {
  tone: 'neutral',
  dot: false,
})
</script>

<template>
  <span class="badge-soft" :class="`badge-soft-${tone}`">
    <span v-if="dot" class="badge-dot"></span><slot />
  </span>
</template>
