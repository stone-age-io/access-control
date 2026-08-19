<script setup lang="ts">
import type { BadgeNavItem, BadgeTabKey } from './badgeNav'

/**
 * The badge's navigation: one flat row of equal columns over `badgeViews()` + the face.
 *
 * # Why equal columns and not DaisyUI `tabs`
 *
 * DaisyUI's `.tabs` is a CSS **grid** whose `.tab` children all carry `grid-row-start: 1`, so it
 * cannot wrap — a `flex-wrap` on it is a no-op. Six items therefore get crushed into one row
 * below their content width, and since `.tab` is a fixed `height: 2rem` with `flex-wrap: wrap`
 * of its own, each tab's icon and label stack inside a 32px box and spill out of it. The
 * operator's preview modal shipped exactly that, mangled, at 448px wide.
 *
 * Equal `flex-1` columns cannot overflow and cannot wrap: whatever the count, they divide the
 * width. Six at 375px are ~62px each, which is what a native tab bar does with five, and no
 * label needs a second line.
 *
 * # Why both tiers use it
 *
 * The holder's shell pins it to the bottom of the viewport, where a phone's primary navigation
 * belongs and where a thumb rests. The operator's preview modal puts the same bar above its
 * footer — so an operator troubleshooting "my badge doesn't work" is looking at the holder's
 * actual navigation over the holder's actual payload, rather than at a desktop lookalike that
 * can disagree with it. The modal used to hand-roll its own tabs, which is how it ended up with
 * a switcher that both looked different and was broken.
 *
 * `min-h-14` with an 11px label is the smallest that holds six items on one line at 375px while
 * clearing the 44px touch floor (see `main.css`). The host supplies edge concerns — `shrink-0`,
 * and `pad-safe-bottom` when the bar really is at the viewport's bottom edge.
 */
defineProps<{
  items: BadgeNavItem[]
  active: BadgeTabKey
}>()

defineEmits<{ select: [BadgeTabKey] }>()
</script>

<template>
  <nav role="tablist" aria-label="Badge sections" class="border-t border-base-300 bg-base-100">
    <div class="mx-auto flex max-w-sm">
      <button
        v-for="item in items"
        :key="item.key"
        type="button"
        role="tab"
        :aria-selected="active === item.key"
        class="relative flex min-h-14 flex-1 flex-col items-center justify-center gap-1 px-0.5 py-1.5 transition-colors"
        :class="active === item.key ? 'text-primary' : 'text-base-content/60'"
        @click="$emit('select', item.key)"
      >
        <!-- The active indicator, sitting ON the divider (`-top-px` covers the 1px border at
             this column). Colour and label weight already mark the active item; a bar is the
             third channel, and the one that reads at a glance without comparing two greys.
             Inset so neighbouring bars cannot touch and be read as one. -->
        <span
          v-if="active === item.key"
          aria-hidden="true"
          class="absolute inset-x-2 -top-px h-0.5 rounded-full bg-primary"
        ></span>
        <!-- The count rides the icon rather than the label: at ~62px a column "Portals 3" would
             wrap, and a number beside a glyph is the shape every phone already uses for a
             count. Neutral, not primary — it is a quantity, not an alert. -->
        <span class="relative inline-flex items-center justify-center">
          <span class="text-lg leading-none" aria-hidden="true">{{ item.icon }}</span>
          <span
            v-if="item.count"
            class="absolute -top-1 left-full -ml-1 rounded-full bg-base-300 px-1 text-[10px] font-semibold leading-4 text-base-content"
          >
            {{ item.count }}
          </span>
        </span>
        <span
          class="text-[11px] leading-none"
          :class="active === item.key ? 'font-semibold' : 'font-medium'"
        >
          {{ item.label }}
        </span>
      </button>
    </div>
  </nav>
</template>
