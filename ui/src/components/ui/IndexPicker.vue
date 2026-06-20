<script setup lang="ts">
/**
 * Availability-aware picker for a logical relay/input index on a controller.
 *
 * Given the controller model's lines (from /api/models) and the current occupancy
 * (the other portals + aux points on the same box), it renders a dropdown of the
 * valid indices, annotates the ones already taken with their owner, and warns when
 * the current selection collides. Value 0 means "not wired / none".
 *
 * When no model profile is available (controller unassigned, or its model unknown),
 * it degrades to a plain number input so the raw index stays editable.
 */
import { computed } from 'vue'
import type { ModelLine } from '@/utils/models'
import { conflictsAt, type Occupant } from '@/utils/io'

const props = withDefaults(
  defineProps<{
    modelValue: number
    /** The model's lines for this kind (relays or inputs); empty = no profile. */
    lines: ModelLine[]
    /** Occupancy for this kind, keyed by index. */
    usage: Map<number, Occupant[]>
    /** This record's id, so its own current index isn't flagged as a conflict. */
    selfId?: string
    /** Label for the 0 / unselected option. */
    noneLabel?: string
  }>(),
  { noneLabel: '— none —' },
)

const emit = defineEmits<{ 'update:modelValue': [number] }>()

const selected = computed<number>({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', Number(v) || 0),
})

const hasProfile = computed(() => props.lines.length > 0)

function others(index: number): Occupant[] {
  return conflictsAt(props.usage, index, props.selfId)
}

const conflict = computed(() => others(selected.value))

function optionText(line: ModelLine): string {
  const taken = others(line.index)
  const base = `${line.index} · ${line.label}`
  return taken.length ? `${base} — in use: ${taken.map((o) => o.label).join(', ')}` : base
}

// A stored value beyond the model's range (e.g. left over from a model change) has
// no option; surface it so it isn't silently dropped.
const known = computed(() => new Set(props.lines.map((l) => l.index)))
const showStale = computed(() => selected.value > 0 && !known.value.has(selected.value))
</script>

<template>
  <div class="flex flex-col gap-1">
    <select
      v-if="hasProfile"
      v-model.number="selected"
      class="select select-bordered"
      :class="conflict.length ? 'select-warning' : ''"
    >
      <option :value="0">{{ noneLabel }}</option>
      <option v-for="line in lines" :key="line.index" :value="line.index">{{ optionText(line) }}</option>
      <option v-if="showStale" :value="selected">{{ selected }} · out of range for this model</option>
    </select>

    <!-- No model profile: keep the raw index editable. -->
    <input
      v-else
      v-model.number="selected"
      type="number"
      min="0"
      class="input input-bordered w-32"
    />

    <p v-if="conflict.length" class="text-xs text-warning">
      Already used by {{ conflict.map((o) => o.label).join(', ') }} on this controller.
    </p>
  </div>
</template>
