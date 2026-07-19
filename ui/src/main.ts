import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import { useAuthStore } from './stores/auth'
import { useBrandingStore } from './stores/branding'
import { useUIStore } from './stores/ui'
import './assets/main.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)

// Theme must be applied before mount to avoid a flash of the wrong theme.
const uiStore = useUIStore()
uiStore.initializeTheme()

// Pre-mount async chain: auth hydration must complete before the first
// navigation (the router guard reads authStore.isAuthenticated, so a valid token
// in localStorage must be restored first), and the operator branding overlay is
// loaded alongside it so the app name/logo are correct on first paint. Each is
// defensively caught so one failure can't block mount.
const authStore = useAuthStore()
const brandingStore = useBrandingStore()
Promise.all([
  authStore.initializeFromAuth().catch(err => console.error('Auth init failed:', err)),
  brandingStore.load().catch(err => console.error('Branding load failed:', err)),
]).finally(() => {
  document.title = brandingStore.appName
  app.mount('#app')
  const appLoader = document.getElementById('app-loader')
  if (appLoader) {
    requestAnimationFrame(() => {
      appLoader.classList.add('fade-out')
      setTimeout(() => appLoader.remove(), 300)
    })
  }
})
