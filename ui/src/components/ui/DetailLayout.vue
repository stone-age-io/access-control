<script setup lang="ts">
/**
 * Single-column detail shell.
 *
 * Header (breadcrumbs + title + #actions + help) above a single column (default
 * slot). Relations live in the column as RelationList sections and the record
 * meta sits at the bottom as a RecordMeta strip — no rail.
 *
 * The column is deliberately UNCAPPED, so it fills MainLayout's max-w-7xl frame
 * exactly as the list views do and list -> detail navigation does not shift. It
 * used to carry max-w-4xl, which was not a measure decision but the ghost of the
 * retired rail: this layout was once a full-frame lg:grid-cols-3 with content in
 * lg:col-span-2, and when the rail went away the column kept roughly its old
 * width and got centered in the space instead of expanding into it. Nothing in a
 * detail view is reading-measure-sensitive (DataField is a compact label over a
 * text-sm value; there is no prose here), while three things in one were being
 * squeezed — the field grids, ControllerIOMap, and a FloorPlanMap that rendered
 * NARROWER on this editable page than the read-only /monitor view of the same
 * plan. FormLayout keeps its max-w-3xl: that cap is earned, because a stretched
 * text input is genuinely worse and its action bar must stay near the last field.
 * So the rule is two widths, not three — wide for looking, narrow for entering.
 */
import HelpButton from './HelpButton.vue'

interface Crumb {
  label: string
  to?: string
}

defineProps<{
  title: string
  subtitle?: string
  breadcrumbs?: Crumb[]
}>()
</script>

<template>
  <div class="w-full">
    <!-- Header -->
    <div class="mb-6">
      <div v-if="breadcrumbs?.length" class="breadcrumbs text-sm">
        <ul>
          <li v-for="(c, i) in breadcrumbs" :key="i">
            <router-link v-if="c.to" :to="c.to">{{ c.label }}</router-link>
            <span v-else class="opacity-70">{{ c.label }}</span>
          </li>
        </ul>
      </div>
      <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
        <div class="min-w-0">
          <h1 class="text-3xl font-bold break-words">{{ title }}</h1>
          <p v-if="subtitle" class="text-base-content/70 mt-1">{{ subtitle }}</p>
        </div>
        <div class="flex items-center gap-2 flex-shrink-0">
          <slot name="actions" />
          <HelpButton />
        </div>
      </div>
    </div>

    <!-- Body -->
    <div class="space-y-6">
      <slot />
    </div>
  </div>
</template>
