import { test, expect } from '@playwright/test'
import { installApiMocks, installDownloadersMocks, ownerMe } from './mocks'

// Hermetic e2e for the two-column Downloaders Settings section.
//
// The adapters mock exposes:
//   - spotDL: supportedGranularities ["track","album"], granularities {track:0, album:0}
//   - Lidarr:  supportedGranularities ["album"],        granularities {album:1}
//
// The Settings page renders a two-column grid:
//   Song column  (data-testid="downloaders-song-col"):  spotDL only
//   Album column (data-testid="downloaders-album-col"): spotDL (order 0), then Lidarr (order 1)
//
// Reordering (clicking "Move down" on spotDL in the Album column) issues two
// PUT /api/v1/adapters/:id requests swapping the album order values — the Song
// column is unaffected (its track orders are unchanged).

// ── helpers ──────────────────────────────────────────────────────────────────

/** Land on the Settings page as an authenticated owner. */
async function goToSettings(page: import('@playwright/test').Page) {
  const authed = { value: true }
  await installApiMocks(page, authed, { me: ownerMe })
  const dl = await installDownloadersMocks(page)

  // The Settings page calls GET /settings; stub it with minimal valid data.
  await page.route('**/api/v1/settings', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' }),
      })
    }
    if (route.request().method() === 'PUT') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' }),
      })
    }
    return route.continue()
  })

  await page.goto('/settings')
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()

  return dl
}

// ── 1) Column layout: Song has spotDL only; Album has spotDL then Lidarr ─────

test('downloaders: Song column shows spotDL only', async ({ page }) => {
  await goToSettings(page)

  const songCol = page.getByTestId('downloaders-song-col')
  await expect(songCol).toBeVisible()

  // The Song column contains exactly one downloader row: spotDL.
  const names = songCol.getByTestId('downloader-name')
  await expect(names).toHaveCount(1)
  await expect(names.first()).toHaveText('spotdl')
})

test('downloaders: Album column shows spotDL first, then Lidarr', async ({ page }) => {
  await goToSettings(page)

  const albumCol = page.getByTestId('downloaders-album-col')
  await expect(albumCol).toBeVisible()

  // The Album column contains exactly two downloader rows: spotDL (order 0) then Lidarr (order 1).
  const names = albumCol.getByTestId('downloader-name')
  await expect(names).toHaveCount(2)
  await expect(names.nth(0)).toHaveText('spotdl')
  await expect(names.nth(1)).toHaveText('lidarr')
})

// ── 2) Disabled controls: first-row up and last-row down are disabled ─────────

test('downloaders: Album column first-row Move up is disabled, last-row Move down is disabled', async ({ page }) => {
  await goToSettings(page)

  const albumCol = page.getByTestId('downloaders-album-col')
  const rows = albumCol.locator('[data-testid="downloader-name"]').locator('..')

  // Get the move buttons scoped inside the album column
  const moveUps = albumCol.getByRole('button', { name: 'Move up' })
  const moveDowns = albumCol.getByRole('button', { name: 'Move down' })

  // First row: Move up is disabled
  await expect(moveUps.first()).toBeDisabled()
  // Last row: Move down is disabled
  await expect(moveDowns.last()).toBeDisabled()

  // Song column: only one row, so both up and down are disabled
  const songCol = page.getByTestId('downloaders-song-col')
  await expect(songCol.getByRole('button', { name: 'Move up' })).toBeDisabled()
  await expect(songCol.getByRole('button', { name: 'Move down' })).toBeDisabled()

  // Suppress unused variable warning
  void rows
})

// ── 3) Reorder Album column: clicking "Move down" on spotDL swaps album orders ──

test('downloaders: clicking Move down on spotDL in Album column issues PUT with swapped album granularities and leaves Song column unchanged', async ({ page }) => {
  const dl = await goToSettings(page)

  const albumCol = page.getByTestId('downloaders-album-col')

  // Capture the two PUT requests that moveInColumn fires concurrently.
  const putBodies: { url: string; body: Record<string, unknown> }[] = []
  page.on('request', (req) => {
    if (req.method() === 'PUT' && req.url().includes('/api/v1/adapters/')) {
      let body: Record<string, unknown> = {}
      try {
        body = JSON.parse(req.postData() ?? '{}') as Record<string, unknown>
      } catch {
        // ignore
      }
      putBodies.push({ url: req.url(), body })
    }
  })

  // Click "Move down" on the FIRST row of the Album column (spotDL, i=0).
  // The button is the second button in the row's flex gap (Move up, Move down).
  const firstRowMoveDown = albumCol.getByRole('button', { name: 'Move down' }).first()
  await expect(firstRowMoveDown).toBeEnabled()
  await firstRowMoveDown.click()

  // Wait for both PUTs to have been captured (moveInColumn fires two concurrent PUTs).
  await page.waitForFunction(() => {
    // The page itself doesn't expose putBodies, so we wait for network idle instead.
    return true
  })
  // Give the network a moment for both PUTs to fire and the mock to handle them.
  await page.waitForTimeout(300)

  // Both PUTs should have been issued.
  expect(putBodies.length).toBeGreaterThanOrEqual(2)

  // Find the spotDL PUT (id spotdl-1) and the Lidarr PUT (id lidarr-1).
  const spotdlPut = putBodies.find((p) => p.url.includes('spotdl-1'))
  const lidarrPut = putBodies.find((p) => p.url.includes('lidarr-1'))
  expect(spotdlPut).toBeDefined()
  expect(lidarrPut).toBeDefined()

  // spotDL's album order should now be 1 (was 0, swapped with Lidarr's 1).
  const spotdlConfig = (spotdlPut!.body.config ?? {}) as Record<string, unknown>
  const spotdlGranularities = (spotdlConfig.granularities ?? {}) as Record<string, number>
  expect(spotdlGranularities.album).toBe(1)

  // Lidarr's album order should now be 0 (was 1, swapped with spotDL's 0).
  const lidarrConfig = (lidarrPut!.body.config ?? {}) as Record<string, unknown>
  const lidarrGranularities = (lidarrConfig.granularities ?? {}) as Record<string, number>
  expect(lidarrGranularities.album).toBe(0)

  // The mock state reflects the swap (statefulness check).
  const adapters = dl.getAdapters()
  const spotdlState = adapters.find((a) => a.id === 'spotdl-1')!
  const lidarrState = adapters.find((a) => a.id === 'lidarr-1')!
  expect(spotdlState.granularities.album).toBe(1)
  expect(lidarrState.granularities.album).toBe(0)

  // The Song column is independent: spotDL's track order is still 0.
  expect(spotdlState.granularities.track).toBe(0)
  // No track order should appear in the Lidarr PUT (lidarr doesn't support track).
  expect(lidarrGranularities.track).toBeUndefined()

  // The Song column in the UI still lists spotDL (unchanged).
  const songCol = page.getByTestId('downloaders-song-col')
  await expect(songCol.getByTestId('downloader-name').first()).toHaveText('spotdl')
})
