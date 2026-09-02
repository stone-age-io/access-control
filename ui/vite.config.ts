import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5174,
    proxy: {
      // Proxy API + admin/auth requests to the embedded PocketBase (accessd).
      '/api': { target: 'http://127.0.0.1:8090', changeOrigin: true },
      '/_': { target: 'http://127.0.0.1:8090', changeOrigin: true },
      // Operator branding overlay (theme.css / branding.json / logo) is served
      // by accessd, so proxy it in dev too.
      '/branding': { target: 'http://127.0.0.1:8090', changeOrigin: true },
    },
  },
  build: {
    // Compiled UI is //go:embed-ed into the accessd binary from
    // internal/webui/public and served by accessd's OnServe SPA route.
    outDir: '../internal/webui/public',
    emptyOutDir: true,
    chunkSizeWarningLimit: 600,
    // Vite 8 bundles with Rolldown, not Rollup. The object form of
    // `manualChunks` is gone, but `codeSplitting.groups` replaces it and stays
    // closer to the old object form than a function would -- a group also
    // captures the dependencies of what it matches
    // (`includeDependenciesRecursively` defaults to true). An earlier comment
    // here claimed rolldown accepts only the function form; it does not, and
    // that claim is why this app shipped with no chunking at all.
    //
    // Each `test` demands a path separator after the package name so a prefix
    // cannot swallow a sibling.
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [
            // MapLibre GL, the vector basemap renderer behind L.maplibreGL.
            // ~900kB on its own: left ungrouped it fell into the useLeafletMap
            // chunk, so every edit to that composable re-hashed a megabyte and
            // every returning user re-downloaded the renderer to pick up a
            // five-line change.
            { name: 'maplibre', test: /[\\/]node_modules[\\/](?:maplibre-gl|@maplibre[\\/][^\\/]+)[\\/]/ },
          ],
        },
      },
    },
  },
})
