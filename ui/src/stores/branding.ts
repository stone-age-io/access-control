import { defineStore } from 'pinia'
import { ref } from 'vue'

// The built-in app name, shown until (and unless) an operator branding overlay
// provides its own. Overridden at runtime by /branding/branding.json.
const DEFAULT_APP_NAME = 'Access Control'

interface BrandingManifest {
  appName?: string
  logo?: string
}

// Operator branding overlay: the app name + logo are fetched once at boot from
// /branding/branding.json (served by accessd from the configured branding.dir,
// with a silent empty {} fallback when unconfigured). The theme colors ride a
// separate /branding/theme.css <link> in index.html — no JS involved.
export const useBrandingStore = defineStore('branding', () => {
  const appName = ref<string>(DEFAULT_APP_NAME)
  const logoUrl = ref<string | null>(null)

  async function load() {
    try {
      const res = await fetch('/branding/branding.json', { cache: 'no-cache' })
      if (res.ok) {
        const manifest = (await res.json()) as BrandingManifest
        if (manifest.appName) appName.value = manifest.appName
        if (manifest.logo) logoUrl.value = `/branding/${manifest.logo}`
      }
    } catch {
      // No branding overlay configured — defaults stand.
    }
  }

  return { appName, logoUrl, load }
})
