<script setup lang="ts" generic="T extends { id: string }">
/**
 * A searchable multi-select for forms — replaces the plain checkbox boxes.
 *
 * `v-model` is an array of selected ids. A search box appears once options pass
 * `searchThreshold`. With `group()` set, options nest under labelled sections
 * (e.g. portals under their location) each with a select-all checkbox that goes
 * indeterminate on a partial selection. A running "N selected" count sits below.
 */
import { ref, computed } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    options: T[]
    /** The stored value for an option; defaults to its id. */
    optionValue?: (o: T) => string
    primary: (o: T) => string
    secondary?: (o: T) => string
    /** When set, options are grouped into sections with per-group select-all. */
    group?: (o: T) => string
    searchText?: (o: T) => string
    empty?: string
    searchThreshold?: number
    searchPlaceholder?: string
  }>(),
  { searchThreshold: 8 },
)

const emit = defineEmits<{ 'update:modelValue': [string[]] }>()

/** Sets a checkbox's indeterminate state (no native attribute for it). */
const vIndeterminate = {
  mounted(el: HTMLInputElement, binding: { value: boolean }) {
    el.indeterminate = binding.value
  },
  updated(el: HTMLInputElement, binding: { value: boolean }) {
    el.indeterminate = binding.value
  },
}

const query = ref('')

function valueOf(o: T): string {
  return props.optionValue ? props.optionValue(o) : o.id
}

const selected = computed(() => new Set(props.modelValue))
const showSearch = computed(() => props.options.length > props.searchThreshold)

function haystack(o: T): string {
  if (props.searchText) return props.searchText(o)
  return [props.primary(o), props.secondary?.(o), props.group?.(o)].filter(Boolean).join(' ')
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.options
  return props.options.filter((o) => haystack(o).toLowerCase().includes(q))
})

interface Grp {
  key: string
  items: T[]
}
const groups = computed<Grp[]>(() => {
  if (!props.group) return [{ key: '', items: filtered.value }]
  const map = new Map<string, T[]>()
  for (const o of filtered.value) {
    const k = props.group(o) || '—'
    const arr = map.get(k)
    if (arr) arr.push(o)
    else map.set(k, [o])
  }
  return [...map.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([key, items]) => ({ key, items }))
})

function toggle(o: T) {
  const v = valueOf(o)
  const next = new Set(props.modelValue)
  if (next.has(v)) next.delete(v)
  else next.add(v)
  emit('update:modelValue', [...next])
}

function groupCount(items: T[]): number {
  let n = 0
  for (const o of items) if (selected.value.has(valueOf(o))) n++
  return n
}

function toggleGroup(items: T[]) {
  const allOn = groupCount(items) === items.length
  const next = new Set(props.modelValue)
  for (const o of items) {
    if (allOn) next.delete(valueOf(o))
    else next.add(valueOf(o))
  }
  emit('update:modelValue', [...next])
}
</script>

<template>
  <div class="space-y-2">
    <input
      v-if="showSearch"
      v-model="query"
      type="search"
      :placeholder="searchPlaceholder || `Filter ${options.length}…`"
      class="input input-bordered input-sm w-full"
    />

    <div class="border border-base-300 rounded-box max-h-72 overflow-y-auto">
      <p v-if="options.length === 0" class="text-sm opacity-50 p-3">
        {{ empty || 'No options available.' }}
      </p>
      <p v-else-if="filtered.length === 0" class="text-sm opacity-50 p-3">
        No matches for “{{ query }}”.
      </p>

      <template v-else>
        <div v-for="g in groups" :key="g.key">
          <!-- Group header (grouped mode only) -->
          <label
            v-if="g.key"
            class="sticky top-0 z-10 flex items-center gap-2 bg-base-200/95 backdrop-blur px-3 py-1.5 cursor-pointer border-b border-base-300"
          >
            <input
              type="checkbox"
              class="checkbox checkbox-xs"
              :checked="groupCount(g.items) === g.items.length"
              v-indeterminate="groupCount(g.items) > 0 && groupCount(g.items) < g.items.length"
              @change="toggleGroup(g.items)"
            />
            <span class="text-[10px] uppercase font-bold tracking-wide opacity-60">{{ g.key }}</span>
            <span class="text-[10px] opacity-40 ml-auto">{{ groupCount(g.items) }}/{{ g.items.length }}</span>
          </label>

          <label
            v-for="o in g.items"
            :key="valueOf(o)"
            class="flex items-center gap-3 cursor-pointer px-3 py-2 hover:bg-base-200"
          >
            <input
              type="checkbox"
              class="checkbox checkbox-sm"
              :checked="selected.has(valueOf(o))"
              @change="toggle(o)"
            />
            <code class="text-sm font-medium">{{ primary(o) }}</code>
            <span v-if="secondary && secondary(o)" class="text-sm opacity-50 truncate">{{ secondary(o) }}</span>
          </label>
        </div>
      </template>
    </div>

    <p class="text-xs opacity-50">{{ modelValue.length }} selected</p>
  </div>
</template>
