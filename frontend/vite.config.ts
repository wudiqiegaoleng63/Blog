import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: process.env.VITE_DEV_API_TARGET || 'http://127.0.0.1:8081', changeOrigin: true },
      '/health': { target: process.env.VITE_DEV_API_TARGET || 'http://127.0.0.1:8081', changeOrigin: true },
    },
  },
  test: {
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
  },
})
