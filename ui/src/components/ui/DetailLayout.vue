<script setup lang="ts">
/**
 * Two-column detail shell.
 *
 * Header (breadcrumbs + title + #actions) above a responsive grid: the main
 * column (default slot, 2/3) and a sticky #rail (1/3) that fills what used to be
 * dead whitespace with context. Create/edit forms use FormLayout instead.
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
  <div class="space-y-6">
    <!-- Header -->
    <div>
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

    <!-- Body: main + rail -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6 items-start">
      <div class="lg:col-span-2 space-y-6 min-w-0">
        <slot />
      </div>
      <aside v-if="$slots.rail" class="lg:col-span-1 space-y-4 lg:sticky lg:top-6">
        <slot name="rail" />
      </aside>
    </div>
  </div>
</template>
