import { ref } from 'vue'

interface ConfirmOptions {
  title?: string
  message: string
  details?: string
  confirmText?: string
  cancelText?: string
  variant?: 'danger' | 'warning' | 'info'
}

interface ConfirmState {
  show: boolean
  options: ConfirmOptions
  resolve: ((value: boolean) => void) | null
}

// Global state — shared across all callers; rendered once by App.vue.
const state = ref<ConfirmState>({
  show: false,
  options: { message: '' },
  resolve: null,
})

/**
 * Promise-based confirmation dialog.
 *
 *   const { confirm } = useConfirm()
 *   if (await confirm({ title: 'Delete', message: '...', variant: 'danger' })) { ... }
 */
export function useConfirm() {
  function confirm(messageOrOptions: string | ConfirmOptions): Promise<boolean> {
    const options: ConfirmOptions =
      typeof messageOrOptions === 'string' ? { message: messageOrOptions } : messageOrOptions

    return new Promise((resolve) => {
      state.value = {
        show: true,
        options: {
          title: options.title || 'Confirm',
          message: options.message,
          details: options.details,
          confirmText: options.confirmText || 'Confirm',
          cancelText: options.cancelText || 'Cancel',
          variant: options.variant || 'danger',
        },
        resolve,
      }
    })
  }

  function handleConfirm() {
    state.value.resolve?.(true)
    state.value.show = false
    state.value.resolve = null
  }

  function handleCancel() {
    state.value.resolve?.(false)
    state.value.show = false
    state.value.resolve = null
  }

  return { confirm, state, handleConfirm, handleCancel }
}
