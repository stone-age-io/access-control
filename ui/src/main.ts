import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'
import { useAuthStore } from './stores/auth'
import { useUIStore } from './stores/ui'
import './assets/main.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)

// Theme must be applied before mount to avoid a flash of the wrong theme.
const uiStore = useUIStore()
uiStore.initializeTheme()

// Auth hydration must complete pre-mount: the router guard reads
// authStore.isAuthenticated on the first navigation, so a valid token sitting
// in localStorage (new tab/window) must be restored before the app renders.
const authStore = useAuthStore()
authStore
  .initializeFromAuth()
  .catch(err => console.error('Auth init failed:', err))
  .finally(() => {
    app.mount('#app')
    const appLoader = document.getElementById('app-loader')
    if (appLoader) {
      requestAnimationFrame(() => {
        appLoader.classList.add('fade-out')
        setTimeout(() => appLoader.remove(), 300)
      })
    }
  })
