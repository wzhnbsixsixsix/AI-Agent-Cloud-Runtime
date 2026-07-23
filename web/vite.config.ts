import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: { proxy: { '/api': { target: 'http://localhost:8086', changeOrigin: true }, '/healthz': { target: 'http://localhost:8086', changeOrigin: true } } }
})
