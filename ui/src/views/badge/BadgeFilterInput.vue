<script setup lang="ts">
/**
 * The one filter box the badge's lists share.
 *
 * Extracted when the portals list grew a filter of its own, because two search boxes a
 * segment apart that look and behave slightly differently is worse than either of them
 * looking however this one does. There is no logic here — each list decides WHAT the query
 * matches and WHETHER it is offered at all (both keep their own threshold); this owns only
 * the input and its clear button.
 *
 * `input-sm` is safe here where `btn-sm` would not be: `main.css` lifts button heights on
 * touch viewports and leaves inputs alone, so this is sized by `min-h-11` directly rather
 * than by a DaisyUI size class that another rule is fighting.
 */
const model = defineModel<string>({ required: true })

defineProps<{
  placeholder: string
  /** Always supplied: the box has no visible label, and "search" alone says nothing. */
  label: string
}>()
</script>

<template>
  <label class="input input-bordered input-sm flex min-h-11 items-center gap-2">
    <span class="opacity-40 text-xs" aria-hidden="true">🔍</span>
    <input
      v-model="model"
      type="search"
      class="grow min-w-0 bg-transparent outline-none text-sm"
      :placeholder="placeholder"
      :aria-label="label"
    />
    <button
      v-if="model"
      type="button"
      class="btn btn-ghost btn-xs btn-circle"
      aria-label="Clear filter"
      @click="model = ''"
    >
      ✕
    </button>
  </label>
</template>
