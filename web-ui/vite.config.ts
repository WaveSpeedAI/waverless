import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 3000,
    // Configure headers to allow iframe embedding and CORS
    headers: {
      'Content-Security-Policy': 'frame-ancestors *',
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, PATCH, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization, X-Requested-With, X-Request-ID',
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true, // Enable WebSocket proxy
      },
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true, // Enable WebSocket proxy
      },
    },
  },
})
