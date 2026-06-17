import { ref } from 'vue'

interface Toast {
  id: number
  message: string
  type: 'success' | 'error' | 'info' | 'warning'
  duration: number
}

// Global state — shared across all callers so toasts survive unmounts.
const toasts = ref<Toast[]>([])
let toastId = 0

/**
 * Simple toast notification system.
 *
 *   const toast = useToast()
 *   toast.success('Saved!')
 *   toast.error('Failed to save')
 */
export function useToast() {
  function show(message: string, type: Toast['type'] = 'info', duration = 3000) {
    const id = toastId++
    toasts.value.push({ id, message, type, duration })
    setTimeout(() => remove(id), duration)
  }

  function remove(id: number) {
    const index = toasts.value.findIndex(t => t.id === id)
    if (index > -1) toasts.value.splice(index, 1)
  }

  const success = (msg: string, duration?: number) => show(msg, 'success', duration ?? 3000)
  const error = (msg: string, duration?: number) => show(msg, 'error', duration ?? 5000)
  const info = (msg: string, duration?: number) => show(msg, 'info', duration ?? 3000)
  const warning = (msg: string, duration?: number) => show(msg, 'warning', duration ?? 4000)

  return { toasts, show, remove, success, error, info, warning }
}
