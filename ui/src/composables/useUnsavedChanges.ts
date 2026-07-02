import { ref, onBeforeUnmount } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useConfirm } from './useConfirm'

/**
 * Warns before navigating away from a form with unsaved edits. Snapshot-based:
 * the state is captured (as JSON) when the composable is created; anything that
 * differs from the snapshot at leave time prompts via the app's confirm dialog.
 *
 *   const { markClean } = useUnsavedChanges(() => form.value)
 *
 * Call markClean() whenever the form matches what's persisted:
 *   - after loadRecord() copies the record into the form (edit mode), and
 *   - after a successful save, before router.push.
 *
 * A beforeunload listener covers tab close / refresh with the browser's own
 * prompt (custom text is not possible there).
 */
export function useUnsavedChanges(getState: () => unknown) {
  const { confirm } = useConfirm()
  const snapshot = ref(JSON.stringify(getState()))

  function markClean() {
    snapshot.value = JSON.stringify(getState())
  }
  function isDirty() {
    return snapshot.value !== JSON.stringify(getState())
  }

  onBeforeRouteLeave(async () => {
    if (!isDirty()) return true
    return confirm({
      title: 'Unsaved changes',
      message: 'Leave without saving?',
      details: 'Your edits on this form will be lost.',
      confirmText: 'Leave',
      cancelText: 'Stay',
      variant: 'warning',
    })
  })

  function onBeforeUnload(e: BeforeUnloadEvent) {
    if (!isDirty()) return
    e.preventDefault()
  }
  window.addEventListener('beforeunload', onBeforeUnload)
  onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))

  return { markClean, isDirty }
}
