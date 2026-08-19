<script setup lang="ts">
import type { BadgeActionResult } from './useBadgeAction'

/**
 * What just happened, under the button it happened to. Renders nothing when there is nothing
 * to say, which — since `useBadgeAction` expires every outcome — is most of the time.
 *
 * One component for all four actions and both surfaces, so the tone, the glyph and the live
 * region cannot drift between the action lists and the floor plan's marker bar.
 *
 * The glyph carries the same information as the colour, because colour alone does not: this is
 * read one-handed outdoors, sometimes by someone who cannot distinguish the two greens DaisyUI
 * uses for success and neutral text. It is `aria-hidden` so a screen reader gets the sentence
 * and not "check mark".
 *
 * `role="status"` announces the outcome without stealing focus — a badge holder pressing an
 * unlock button needs to be TOLD it worked, not to discover it by hunting back down the page.
 */
defineProps<{ result?: BadgeActionResult }>()
</script>

<template>
  <p
    v-if="result"
    role="status"
    aria-live="polite"
    class="text-xs"
    :class="result.ok ? 'text-success' : 'text-error'"
  >
    <span aria-hidden="true">{{ result.ok ? '✓' : '✕' }}</span>
    {{ result.message }}
  </p>
</template>
