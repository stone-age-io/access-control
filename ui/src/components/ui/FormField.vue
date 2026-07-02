<script setup lang="ts">
/**
 * Standard form field wrapper: a label (with an optional required mark) above the
 * control (default slot), with optional helper text below. One component for every
 * form so labels, spacing, and hint styling are identical everywhere.
 *
 *   <FormField label="Code" required hint="Stable slug…">
 *     <input v-model="form.code" class="input input-bordered font-mono" />
 *   </FormField>
 *
 * Pass `inline` for a toggle/checkbox row — the control sits before the label on
 * one clickable line, with the hint beneath:
 *
 *   <FormField inline label="Recurring" hint="Matches the same day every year.">
 *     <input v-model="form.recurring" type="checkbox" class="toggle toggle-primary" />
 *   </FormField>
 *
 * Use the #hint slot for rich/conditional helper text (e.g. a warning); it falls
 * back to the `hint` prop. Conditional inline warnings can also just live in the
 * default slot after the control.
 *
 * Pass `error` (a message, usually from the view's validate()) to flag the field:
 * the control gets a red border and the message renders below it. Errors are set
 * on submit and cleared by the next validate pass.
 */
defineProps<{
  label?: string
  required?: boolean
  hint?: string
  inline?: boolean
  error?: string
}>()
</script>

<template>
  <div class="flex flex-col gap-1.5" :class="{ 'field-invalid': error }">
    <!-- Inline (toggle / checkbox): control then label on one row -->
    <label v-if="inline" class="flex items-center gap-3 cursor-pointer">
      <slot />
      <span class="text-sm font-medium">
        <slot name="label">{{ label }}</slot>
        <span v-if="required" class="text-error ml-0.5" aria-hidden="true">*</span>
      </span>
    </label>

    <!-- Stacked (default): label above control -->
    <template v-else>
      <label v-if="label || $slots.label" class="text-sm font-medium text-base-content/90">
        <slot name="label">{{ label }}</slot>
        <span v-if="required" class="text-error ml-0.5" aria-hidden="true">*</span>
      </label>
      <slot />
    </template>

    <!-- Validation error (from the view's submit-time validate) -->
    <p v-if="error" class="text-xs leading-relaxed text-error" role="alert">{{ error }}</p>

    <!-- Helper text -->
    <p v-if="hint || $slots.hint" class="text-xs leading-relaxed text-base-content/60">
      <slot name="hint">{{ hint }}</slot>
    </p>
  </div>
</template>

<style scoped>
/* The control lives in the slot, so the red border rides a class on the wrapper. */
.field-invalid :deep(.input),
.field-invalid :deep(.select),
.field-invalid :deep(.textarea) {
  @apply border-error focus:border-error;
}
</style>
