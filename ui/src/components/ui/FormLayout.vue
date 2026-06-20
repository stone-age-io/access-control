<script setup lang="ts">
/**
 * Single-column create/edit form shell.
 *
 * Header (breadcrumbs + title + Help) above a centered column of cards (default
 * slot), with an optional inline policy-KV-key hint and a sticky action bar
 * (#actions). Wrap the call site in a <form> and put Cancel + submit in
 * #actions — list Cancel first; the bar reverses order on mobile so the primary
 * action sits on top and both buttons go full-width.
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
  kvKey?: string
  kvPlaceholder?: string
}>()
</script>

<template>
  <div class="mx-auto w-full max-w-3xl">
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
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <h1 class="text-3xl font-bold break-words">{{ title }}</h1>
          <p v-if="subtitle" class="text-base-content/70 mt-1">{{ subtitle }}</p>
        </div>
        <HelpButton />
      </div>

      <!-- Inline policy KV key (replaces the old rail card) -->
      <div v-if="kvKey || kvPlaceholder" class="mt-3 flex flex-wrap items-center gap-2 text-xs">
        <span class="font-semibold uppercase tracking-wide text-base-content/50">Policy KV key</span>
        <code
          class="font-mono bg-base-200 px-2 py-0.5 rounded break-all"
          :class="{ 'opacity-50': !kvKey }"
        >{{ kvKey || kvPlaceholder }}</code>
      </div>
    </div>

    <!-- Cards -->
    <div class="space-y-6">
      <slot />
    </div>

    <!-- Sticky action bar -->
    <div
      v-if="$slots.actions"
      class="sticky bottom-0 z-20 mt-6 py-3 flex flex-col-reverse sm:flex-row sm:justify-end gap-2 sm:gap-3 border-t border-base-300 bg-base-100/95 backdrop-blur"
    >
      <slot name="actions" />
    </div>
  </div>
</template>
