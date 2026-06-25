import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useUIStore = defineStore('ui', () => {
  // State
  const theme = ref<'light' | 'dark'>('dark')
  const sidebarCompact = ref(false) // desktop compact (icons-only) mode
  // Live Map: preferred way to view a location — the Leaflet floor plan or the
  // door-card list. Persisted so an operator who prefers one isn't re-toggling at
  // every building. Locations with no floor plan always show the list regardless.
  const monitorViewMode = ref<'plan' | 'list'>('plan')

  function toggleTheme() {
    theme.value = theme.value === 'light' ? 'dark' : 'light'
    document.documentElement.setAttribute('data-theme', theme.value)
    localStorage.setItem('theme', theme.value)
  }

  function toggleCompact() {
    sidebarCompact.value = !sidebarCompact.value
    localStorage.setItem('sidebar_compact', String(sidebarCompact.value))
  }

  function setMonitorViewMode(mode: 'plan' | 'list') {
    monitorViewMode.value = mode
    localStorage.setItem('monitor_view_mode', mode)
  }

  function initializeTheme() {
    const saved = localStorage.getItem('theme') as 'light' | 'dark' | null
    if (saved) theme.value = saved
    document.documentElement.setAttribute('data-theme', theme.value)

    const savedCompact = localStorage.getItem('sidebar_compact')
    if (savedCompact) sidebarCompact.value = savedCompact === 'true'

    const savedView = localStorage.getItem('monitor_view_mode')
    if (savedView === 'plan' || savedView === 'list') monitorViewMode.value = savedView
  }

  return {
    theme,
    sidebarCompact,
    monitorViewMode,
    toggleTheme,
    toggleCompact,
    setMonitorViewMode,
    initializeTheme,
  }
})
