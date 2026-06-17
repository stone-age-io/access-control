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
    },
  },
  build: {
    // Compiled UI is served by accessd's embedded PocketBase from pb_public/.
    outDir: '../pb_public',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks: {
          'vue-vendor': ['vue', 'vue-router', 'pinia'],
        },
      },
    },
    chunkSizeWarningLimit: 600,
  },
})
