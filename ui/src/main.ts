import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import { useAuthStore } from './stores/auth'
import { useBadgeAuthStore } from './stores/badgeAuth'
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
// The badge session is hydrated alongside the operator one, for the same reason:
// the router guard reads badgeAuth.isAuthenticated on a /badge navigation, so a
// token in localStorage must be restored before the first navigation. The two are
// independent sessions (separate clients, separate storage keys), so both are
// restored and neither disturbs the other.
const authStore = useAuthStore()
const badgeAuthStore = useBadgeAuthStore()
const brandingStore = useBrandingStore()
Promise.all([
  authStore.initializeFromAuth().catch(err => console.error('Auth init failed:', err)),
  badgeAuthStore.initialize().catch(err => console.error('Badge auth init failed:', err)),
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

/**
 * Register the service worker, which exists only so the browser offers "Install".
 * It caches nothing (see ui/public/sw.js) — this app must never show a stale answer to
 * "what does my badge open right now".
 *
 * PROD only: in dev, Vite serves the modules a worker would sit in front of, and a
 * registration survives a server restart, so it is a reliable way to spend an afternoon
 * debugging a stale bundle.
 */
if ('serviceWorker' in navigator && import.meta.env.PROD) {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js')
      .then((reg) => console.log('SW registered:', reg.scope))
      .catch((err) => console.error('SW registration failed:', err))
  })
}
