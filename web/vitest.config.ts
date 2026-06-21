import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/setupTests.ts',
    // Unit tests live under src/. The e2e/ specs are Playwright (run via `npm run
    // e2e`), not Vitest — exclude them so a full `vitest run` doesn't try to load
    // @playwright/test's `test` runner.
    include: ['src/**/*.{test,spec}.{ts,tsx}'],
    exclude: ['e2e/**', 'node_modules/**'],
  },
})
