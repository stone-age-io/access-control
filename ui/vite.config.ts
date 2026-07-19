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
    // Vite 8 bundles with rolldown; default chunking is fine for an app this
    // size (no manualChunks object — rolldown only accepts the function form).
    chunkSizeWarningLimit: 600,
  },
})
