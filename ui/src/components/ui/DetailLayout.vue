<script setup lang="ts">
/**
 * Single-column detail shell.
 *
 * Header (breadcrumbs + title + #actions + help) above a centered single column
 * (default slot). Relations live in the column as RelationList sections and the
 * record meta sits at the bottom as a RecordMeta strip — no rail. Matches the
 * single-column FormLayout so detail and form views read the same.
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
  <div class="mx-auto w-full max-w-4xl">
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
