import L from 'leaflet'
import iconRetinaUrl from 'leaflet/dist/images/marker-icon-2x.png'
import iconUrl from 'leaflet/dist/images/marker-icon.png'
import shadowUrl from 'leaflet/dist/images/marker-shadow.png'

/**
 * Leaflet's default marker icons break when bundled: Leaflet derives their URLs
 * from its own asset path, which Vite rewrites, so markers render as broken
 * images (notably question marks on iOS Safari). We import the icon files so
 * Vite fingerprints and bundles them, then point Leaflet's default icon at the
 * resolved URLs. No CDN dependency — everything ships in the embedded UI.
 *
 * Mutates the global L.Icon.Default singleton, so it's idempotent and safe to
 * call from every map composable.
 */
export function fixLeafletIcons() {
  // @ts-ignore - _getIconUrl is an internal Leaflet method
  delete L.Icon.Default.prototype._getIconUrl
  L.Icon.Default.mergeOptions({ iconRetinaUrl, iconUrl, shadowUrl })
}
