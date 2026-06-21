<script setup lang="ts">
/**
 * Shell for the collection list pages: header (title + subtitle + actions, with a
 * contextual Help button auto-included), an optional search bar / toolbar, and the
 * loading / error / empty state machine. The loaded list + pagination go in the
 * default slot.
 *
 *   <ListLayout title="Portals" v-model:search="q" search-placeholder="Search…"
 *     :loading="loading" :error="error" :is-empty="items.length === 0" :has-query="!!q"
 *     empty-icon="🚪" empty-title="No portals yet" empty-message="…"
 *     error-title="Failed to load portals" @retry="reload">
 *     <template #actions> <router-link …>New Portal</router-link> </template>
 *     <template #empty-action> <router-link …>Create Portal</router-link> </template>
 *     <BaseCard :no-padding="true"> … list … <ListPagination …/> </BaseCard>
 *   </ListLayout>
 */
import EmptyState from './EmptyState.vue'
import ErrorState from './ErrorState.vue'
import HelpButton from './HelpButton.vue'

defineProps<{
  title: string
  subtitle?: string
  search?: string
  searchPlaceholder?: string
  loading?: boolean
  error?: string | null
  isEmpty?: boolean
  hasQuery?: boolean
  emptyIcon?: string
  emptyTitle?: string
  emptyMessage?: string
  errorTitle?: string
}>()

const emit = defineEmits<{ 'update:search': [value: string]; retry: [] }>()

function onInput(e: Event) {
  emit('update:search', (e.target as HTMLInputElement).value)
}
</script>

<template>
  <div class="space-y-6">
    <!-- Header -->
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">{{ title }}</h1>
        <p v-if="subtitle" class="text-base-content/70 mt-1">{{ subtitle }}</p>
      </div>
      <div class="flex items-center gap-2 w-full sm:w-auto">
        <slot name="actions" />
        <HelpButton />
      </div>
    </div>

    <!-- Toolbar + search -->
    <div v-if="searchPlaceholder || $slots.toolbar" class="flex flex-col sm:flex-row gap-3">
      <slot name="toolbar" />
      <label v-if="searchPlaceholder" class="input input-bordered flex items-center gap-2 flex-1">
        <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 opacity-40 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-4.35-4.35m1.35-5.65a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input :value="search" type="text" class="grow" :placeholder="searchPlaceholder" @input="onInput" />
        <button v-if="search" type="button" class="shrink-0 -mr-2 flex h-11 w-11 items-center justify-center rounded-full opacity-40 hover:opacity-100 transition-opacity" aria-label="Clear search" @click="emit('update:search', '')">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </label>
    </div>

    <!-- State machine -->
    <div v-if="loading && isEmpty" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>
    <ErrorState v-else-if="error && isEmpty" :title="errorTitle || 'Failed to load'" :message="error" @retry="emit('retry')" />
    <EmptyState v-else-if="isEmpty && !hasQuery" :icon="emptyIcon" :title="emptyTitle || 'Nothing here yet'" :message="emptyMessage">
      <template v-if="$slots['empty-action']" #action><slot name="empty-action" /></template>
    </EmptyState>
    <slot v-else />
  </div>
</template>
