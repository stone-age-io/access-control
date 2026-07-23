<script setup lang="ts" generic="T extends { id: string }">
/**
 * A searchable list of related records, rendered as a card in the main column.
 *
 * Replaces the old rail RefList and the ad-hoc main-column <ul> lists so every
 * relation looks and behaves the same: accessible <router-link> rows, a count
 * badge, an optional `+ Add` action slot, a search box that appears once the
 * list passes `searchThreshold`, and optional `group()` sectioning (e.g. portals
 * under their location). Rich rows can override the default row via the scoped
 * `#item` slot; the link wrapper, hover, and dividers stay consistent.
 */
import { ref, computed } from 'vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const props = withDefaults(
  defineProps<{
    title: string
    icon?: string
    items: T[]
    to: (item: T) => string
    /** Default row content; optional when using the #item slot. */
    primary?: (item: T) => string
    secondary?: (item: T) => string
    /** What the search box filters on; defaults to primary + secondary. */
    searchText?: (item: T) => string
    /** When set, rows are grouped into labelled sections by the returned key. */
    group?: (item: T) => string
    hint?: string
    empty?: string
    loading?: boolean
    searchThreshold?: number
    searchPlaceholder?: string
  }>(),
  { searchThreshold: 8 },
)

const query = ref('')

const showSearch = computed(() => !props.loading && props.items.length > props.searchThreshold)

function haystack(item: T): string {
  if (props.searchText) return props.searchText(item)
  return [props.primary?.(item), props.secondary?.(item)].filter(Boolean).join(' ')
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter((i) => haystack(i).toLowerCase().includes(q))
})

interface Group {
  key: string
  items: T[]
}
const groups = computed<Group[]>(() => {
  if (!props.group) return [{ key: '', items: filtered.value }]
  const map = new Map<string, T[]>()
  for (const item of filtered.value) {
    const k = props.group(item) || '—'
    const arr = map.get(k)
    if (arr) arr.push(item)
    else map.set(k, [item])
  }
  return [...map.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([key, items]) => ({ key, items }))
})
</script>

<template>
  <div class="card bg-base-100 border border-base-300 shadow-sm">
    <div class="card-body gap-3">
      <!-- Header -->
      <div class="flex items-center justify-between gap-2">
        <h2 class="card-title text-base flex items-center gap-2">
          <span v-if="icon">{{ icon }}</span>{{ title }}
          <SoftBadge v-if="!loading">{{ items.length }}</SoftBadge>
        </h2>
        <div v-if="$slots.actions" class="flex items-center gap-2">
          <slot name="actions" />
        </div>
      </div>

      <p v-if="hint" class="text-sm text-base-content/60 -mt-1">{{ hint }}</p>

      <!-- Search -->
      <input
        v-if="showSearch"
        v-model="query"
        type="search"
        :placeholder="searchPlaceholder || `Filter ${items.length}…`"
        class="input input-bordered input-sm w-full"
      />

      <!-- States -->
      <div v-if="loading" class="py-2">
        <span class="loading loading-dots loading-sm opacity-40"></span>
      </div>
      <p v-else-if="items.length === 0" class="text-sm opacity-50 py-2">{{ empty || 'None' }}</p>
      <p v-else-if="filtered.length === 0" class="text-sm opacity-50 py-2">No matches for “{{ query }}”.</p>

      <!-- List -->
      <div v-else class="space-y-4">
        <div v-for="g in groups" :key="g.key">
          <div
            v-if="g.key"
            class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-1 px-1"
          >
            {{ g.key }}
          </div>
          <ul class="divide-y divide-base-200">
            <li v-for="item in g.items" :key="item.id">
              <router-link
                :to="to(item)"
                class="flex items-center gap-3 py-2.5 px-2 -mx-2 rounded hover:bg-base-200 transition-colors"
              >
                <slot name="item" :item="item">
                  <code class="text-sm font-medium text-primary truncate">{{ primary?.(item) }}</code>
                  <span v-if="secondary && secondary(item)" class="text-sm opacity-60 truncate flex-1">
                    {{ secondary(item) }}
                  </span>
                </slot>
              </router-link>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>
