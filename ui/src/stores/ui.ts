import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useUIStore = defineStore('ui', () => {
  // State
  const theme = ref<'light' | 'dark'>('dark')
  const sidebarCompact = ref(false) // desktop compact (icons-only) mode

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
    document.documentElement.setAttribute('data-theme', theme.value)
    localStorage.setItem('theme', theme.value)
  }

  function toggleCompact() {
    sidebarCompact.value = !sidebarCompact.value
    localStorage.setItem('sidebar_compact', String(sidebarCompact.value))
  }

  function initializeTheme() {
    const saved = localStorage.getItem('theme') as 'light' | 'dark' | null
    if (saved) theme.value = saved
    document.documentElement.setAttribute('data-theme', theme.value)

    const savedCompact = localStorage.getItem('sidebar_compact')
    if (savedCompact) sidebarCompact.value = savedCompact === 'true'
  }

  return { theme, sidebarCompact, toggleTheme, toggleCompact, initializeTheme }
})
