<template>
  <Teleport to="body">
    <div v-if="modelValue" class="confirm-overlay" @click.self="cancel">
      <div class="confirm-dialog" :class="variantClass">
        <div class="confirm-header">
          <div class="confirm-icon">{{ icon }}</div>
          <h3 class="confirm-title">{{ title }}</h3>
        </div>

        <div class="confirm-body">
          <p class="confirm-message">{{ message }}</p>
          <p v-if="details" class="confirm-details">{{ details }}</p>
        </div>

        <div class="confirm-actions">
          <button class="btn-secondary" @click="cancel">{{ cancelText }}</button>
          <button class="btn-confirm" :class="variantClass" @click="confirm" autofocus>
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

const variantClass = computed(() => `variant-${props.variant}`)

function confirm() {
  emit('confirm')
  emit('update:modelValue', false)
}

function cancel() {
  emit('cancel')
  emit('update:modelValue', false)
}
</script>

<style scoped>
.confirm-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10100;
  backdrop-filter: blur(2px);
  animation: fadeIn 0.2s ease-out;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.confirm-dialog {
  background: oklch(var(--b1));
  border: 2px solid;
  border-radius: 8px;
  width: 90%;
  max-width: 480px;
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04);
  animation: slideUp 0.2s ease-out;
}

@keyframes slideUp {
  from { transform: translateY(20px); opacity: 0; }
  to { transform: translateY(0); opacity: 1; }
}

.confirm-dialog.variant-danger { border-color: oklch(var(--er)); }
.confirm-dialog.variant-warning { border-color: oklch(var(--wa)); }
.confirm-dialog.variant-info { border-color: oklch(var(--in)); }

.confirm-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 20px 24px;
  border-bottom: 1px solid oklch(var(--b3));
}

.confirm-icon {
  font-size: 32px;
  line-height: 1;
}

.confirm-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: oklch(var(--bc));
}

.confirm-body {
  padding: 24px;
}

.confirm-message {
  margin: 0 0 12px 0;
  font-size: 15px;
  line-height: 1.5;
  color: oklch(var(--bc));
}

.confirm-details {
  margin: 0;
  font-size: 13px;
  line-height: 1.4;
  color: oklch(var(--bc) / 0.6);
  padding: 12px;
  background: rgba(0, 0, 0, 0.2);
  border-radius: 4px;
}

.confirm-actions {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
  padding: 16px 24px;
  border-top: 1px solid oklch(var(--b3));
}

.btn-secondary,
.btn-confirm {
  padding: 10px 20px;
  border-radius: 6px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  border: none;
}

.btn-secondary {
  background: oklch(var(--bc) / 0.1);
  color: oklch(var(--bc));
}

.btn-secondary:hover {
  background: oklch(var(--bc) / 0.15);
}

.btn-confirm {
  color: white;
}

.btn-confirm.variant-danger { background: oklch(var(--er)); }
.btn-confirm.variant-danger:hover { background: oklch(var(--er) / 0.8); }

.btn-confirm.variant-warning { background: oklch(var(--wa)); }
.btn-confirm.variant-warning:hover { background: oklch(var(--wa) / 0.8); }

.btn-confirm.variant-info { background: oklch(var(--in)); }
.btn-confirm.variant-info:hover { background: oklch(var(--in) / 0.8); }

@media (max-width: 600px) {
  .confirm-dialog {
    width: 95%;
    max-width: none;
  }
  .confirm-header { padding: 16px 20px; }
  .confirm-body { padding: 20px; }
  .confirm-actions {
    flex-direction: column-reverse;
    padding: 12px 20px;
  }
  .btn-secondary,
  .btn-confirm { width: 100%; }
}
</style>
