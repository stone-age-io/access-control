<template>
  <Teleport to="body">
    <!-- z-index sits above toasts (App.vue) so a confirm is never occluded. -->
    <div
      v-if="modelValue"
      class="modal modal-open"
      style="z-index: 10100"
      @click.self="cancel"
    >
      <div class="modal-box border-t-4" :class="borderClass">
        <div class="flex items-center gap-3">
          <span class="text-2xl leading-none" aria-hidden="true">{{ icon }}</span>
          <h3 class="text-lg font-bold">{{ title }}</h3>
        </div>

        <p class="py-3 text-base-content/90">{{ message }}</p>
        <p v-if="details" class="text-sm text-base-content/60 bg-base-200 rounded-lg p-3">
          {{ details }}
        </p>

        <div class="modal-action max-sm:flex-col-reverse">
          <button class="btn btn-ghost max-sm:w-full" @click="cancel">{{ cancelText }}</button>
          <button class="btn max-sm:w-full" :class="confirmClass" @click="confirm" autofocus>
            {{ confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  modelValue: boolean
  title: string
  message: string
  details?: string
  confirmText?: string
  cancelText?: string
  variant?: 'danger' | 'warning' | 'info'
}

const props = withDefaults(defineProps<Props>(), {
  confirmText: 'Confirm',
  cancelText: 'Cancel',
  variant: 'danger',
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  confirm: []
  cancel: []
}>()

const icon = computed(() => {
  switch (props.variant) {
    case 'danger': return '⚠️'
    case 'warning': return '⚡'
    case 'info': return 'ℹ️'
    default: return '⚠️'
  }
})

// The variant tints the top accent stripe + the confirm button (a solid CTA is
// right for a destructive confirm — buttons, not the soft-badge treatment).
const borderClass = computed(
  () => ({ danger: 'border-error', warning: 'border-warning', info: 'border-info' })[props.variant] || 'border-error',
)
const confirmClass = computed(
  () => ({ danger: 'btn-error', warning: 'btn-warning', info: 'btn-info' })[props.variant] || 'btn-error',
)

function confirm() {
  emit('confirm')
  emit('update:modelValue', false)
}

function cancel() {
  emit('cancel')
  emit('update:modelValue', false)
}
</script>
