import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// dev: npm run dev, backend on :8090; requests are proxied
export default defineConfig({
  plugins: [react()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': 'http://localhost:8090',
      '/media': 'http://localhost:8090',
    },
  },
})
