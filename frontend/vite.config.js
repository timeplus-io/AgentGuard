import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig(({ mode }) => {
  // loadEnv makes .env/.env.local values visible here; the config file itself
  // only sees shell-exported vars via process.env.
  const env = loadEnv(mode, process.cwd(), '')
  return {
    plugins: [react()],
    server: {
      proxy: {
        '/api': env.VITE_API_PROXY || 'http://localhost:8080'
      }
    }
  }
})
