import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The Orchestrator serves the built UI and the API from one origin. In dev the
// browser talks to this server only, and /api (including log WebSockets) is
// proxied to the Orchestrator, so the app is same-origin here too and no CORS
// is needed. Point at a different Orchestrator with ORCHESTRATOR_URL.
const orchestrator = process.env.ORCHESTRATOR_URL ?? 'http://127.0.0.1:9090'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5183,
    strictPort: true,
    proxy: {
      '/api': { target: orchestrator, ws: true },
    },
  },
})
