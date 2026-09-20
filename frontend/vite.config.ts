import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:2345', changeOrigin: true },
      '/healthz': { target: 'http://127.0.0.1:2345', changeOrigin: true },
      '/ws': { target: 'ws://127.0.0.1:2345', changeOrigin: true, ws: true },
    },
  },
})
