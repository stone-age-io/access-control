import { ref } from 'vue'

// Global open state — the panel lives once in MainLayout; triggers (HelpButton,
// HelpFab) live wherever the page renders them. Content is resolved from the
// active route at render time (see help/registry.ts), so this only tracks open/closed.
const isOpen = ref(false)

export function useHelp() {
  const open = () => { isOpen.value = true }
  const close = () => { isOpen.value = false }
  const toggle = () => { isOpen.value = !isOpen.value }
  return { isOpen, open, close, toggle }
}
