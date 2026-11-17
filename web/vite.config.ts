import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  base: '/gem/',
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/gem/api': {
        target: 'http://localhost:8080',
        rewrite: (path: string) => path.replace(/^\/gem/, ''),
      },
      '/gem/ws': { target: 'ws://localhost:8080', ws: true, rewrite: (path: string) => path.replace(/^\/gem/, '') },
    },
  },
  test: {
    environment: 'node',
  },
} as any)
