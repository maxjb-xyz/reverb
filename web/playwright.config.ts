import { defineConfig, devices } from '@playwright/test'

// Hermetic e2e: serve the built SPA via `vite preview` and intercept ALL
// /api/v1/* HTTP + the /api/v1/ws WebSocket in the spec. No real backend.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://localhost:4173',
    trace: 'on-first-retry',
    // PlayerBar is `hidden md:flex`; use a desktop viewport so it renders.
    viewport: { width: 1280, height: 800 },
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    // `vite preview` serves the production build on :4173 (Vite's default
    // preview port). Build first so dist exists.
    command: 'npm run build && npm run preview -- --port 4173 --strictPort',
    url: 'http://localhost:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
