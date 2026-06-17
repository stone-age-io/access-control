<script setup lang="ts">
/**
 * Two-column detail / form shell.
 *
 * Header (breadcrumbs + title + #actions) above a responsive grid: the main
 * column (default slot, 2/3) and a sticky #rail (1/3) that fills what used to be
 * dead whitespace with context. An optional #footer renders as a sticky action
 * bar — wrap the whole thing in a <form> and put the submit button there.
 */
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
        <div v-if="$slots.actions" class="flex items-center gap-2 flex-shrink-0">
          <slot name="actions" />
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

    <!-- Sticky action bar (forms) -->
    <div
      v-if="$slots.footer"
      class="sticky bottom-0 z-10 -mx-4 -mb-4 lg:-mx-6 lg:-mb-6 px-4 lg:px-6 py-3 flex flex-col sm:flex-row justify-end gap-2 sm:gap-3 border-t border-base-300 bg-base-100/85 backdrop-blur"
    >
      <slot name="footer" />
    </div>
  </div>
</template>
