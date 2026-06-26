<script setup lang="ts" generic="T">
/**
 * A single-select, type-to-filter combobox — the searchable replacement for a
 * native <select> when there are too many options to scroll (credentials,
 * portals). v-model is the selected value (via `optionValue`); the dropdown filters
 * as you type, supports keyboard nav (↑/↓/Enter/Esc), and is clearable.
 *
 *   <Combobox v-model="portal" :options="portals"
 *     :option-value="p => p.code" :primary="p => p.code" :secondary="p => p.name"
 *     placeholder="Pick a portal…" />
 */
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'

const props = withDefaults(
  defineProps<{
    modelValue: string
    options: T[]
    /** The stored value for an option. */
    optionValue: (o: T) => string
    /** Primary label (the prominent text). */
    primary: (o: T) => string
    /** Optional secondary label (dimmed, after the primary). */
    secondary?: (o: T) => string
    /** Text searched against; defaults to primary + secondary. */
    searchText?: (o: T) => string
    placeholder?: string
    clearable?: boolean
  }>(),
  { clearable: true, placeholder: 'Search…' },
)

const emit = defineEmits<{ 'update:modelValue': [string] }>()

const root = ref<HTMLElement | null>(null)
const query = ref('')
const open = ref(false)
const highlight = ref(0)

const selected = computed(() => props.options.find((o) => props.optionValue(o) === props.modelValue) || null)

// While closed, the input shows the selected label; while open it shows what the
// user is typing. Keep it synced to external model changes when closed.
watch(
  () => props.modelValue,
  () => {
    if (!open.value) query.value = selected.value ? props.primary(selected.value) : ''
  },
  { immediate: true },
)

function haystack(o: T): string {
  if (props.searchText) return props.searchText(o)
  return [props.primary(o), props.secondary?.(o)].filter(Boolean).join(' ')
}

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!open.value || !q) return props.options
  return props.options.filter((o) => haystack(o).toLowerCase().includes(q))
})

function openList() {
  if (open.value) return
  open.value = true
  query.value = '' // clear so the full list shows and the user can type to filter
  highlight.value = 0
}

function revert() {
  open.value = false
  query.value = selected.value ? props.primary(selected.value) : ''
}

function choose(o: T) {
  emit('update:modelValue', props.optionValue(o))
  query.value = props.primary(o)
  open.value = false
}

function clear() {
  emit('update:modelValue', '')
  query.value = ''
  open.value = false
}

function onInput() {
  open.value = true
  highlight.value = 0
}

function onKeydown(e: KeyboardEvent) {
  if (!open.value) {
    if (e.key === 'ArrowDown' || e.key === 'Enter') {
      openList()
      e.preventDefault()
    }
    return
  }
  switch (e.key) {
    case 'ArrowDown':
      highlight.value = Math.min(highlight.value + 1, filtered.value.length - 1)
      e.preventDefault()
      break
    case 'ArrowUp':
      highlight.value = Math.max(highlight.value - 1, 0)
      e.preventDefault()
      break
    case 'Enter': {
      const o = filtered.value[highlight.value]
      if (o) choose(o)
      e.preventDefault()
      break
    }
    case 'Escape':
      revert()
      e.preventDefault()
      break
  }
}

// Close (reverting any half-typed text) on a click outside the component.
function onDocMouseDown(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) revert()
}
onMounted(() => document.addEventListener('mousedown', onDocMouseDown))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocMouseDown))
</script>

<template>
  <div ref="root" class="relative">
    <div class="flex items-center gap-1">
      <input
        v-model="query"
        type="text"
        role="combobox"
        :aria-expanded="open"
        autocomplete="off"
        class="input input-bordered w-full"
        :placeholder="placeholder"
        @focus="openList"
        @input="onInput"
        @keydown="onKeydown"
      />
      <button
        v-if="clearable && modelValue"
        type="button"
        class="btn btn-ghost btn-sm btn-square absolute right-1"
        aria-label="Clear selection"
        @mousedown.prevent="clear"
      >
        ✕
      </button>
    </div>

    <ul
      v-if="open"
      role="listbox"
      class="absolute z-[600] mt-1 w-full bg-base-100 border border-base-300 rounded-box shadow-lg max-h-72 overflow-y-auto"
    >
      <li v-if="filtered.length === 0" class="px-3 py-2 text-sm opacity-50">
        No matches{{ query ? ` for “${query}”` : '' }}.
      </li>
      <li
        v-for="(o, i) in filtered"
        :key="optionValue(o)"
        role="option"
        :aria-selected="optionValue(o) === modelValue"
        class="flex items-center gap-2 px-3 py-2 cursor-pointer"
        :class="i === highlight ? 'bg-base-200' : 'hover:bg-base-200/60'"
        @mousedown.prevent="choose(o)"
        @mouseenter="highlight = i"
      >
        <span class="font-mono text-sm">{{ primary(o) }}</span>
        <span v-if="secondary && secondary(o)" class="text-sm opacity-50 truncate">{{ secondary(o) }}</span>
        <span v-if="optionValue(o) === modelValue" class="ml-auto text-primary">✓</span>
      </li>
    </ul>
  </div>
</template>
