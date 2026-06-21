import { test, expect } from '@playwright/test'
import { installApiMocks, installWsMock, externalTrack } from './mocks'

test('core loop: login -> search everywhere -> download -> in-library -> play', async ({ page }) => {
  const authed = { value: false }
  // Install HTTP mocks first (GET /downloads returns [] so no pre-existing job).
  await installApiMocks(page, authed)
  // Install WS mock and get the trigger object; does NOT send any frame yet.
  const ws = await installWsMock(page)

  // 1) Load: setup not required, not authed -> Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Log in to Crate' })).toBeVisible()

  // 2) Log in. The app reloads on success; /me now returns authed. After the
  //    reload the realtime socket opens and resyncs the download list exactly once
  //    (GET /downloads). Wait for that initial resync to settle BEFORE we enqueue,
  //    so its (empty) result can't land late and clobber the completed job. (In
  //    production the resync returns the real server state, so this race is mock-only.)
  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)
  await page.getByPlaceholder('Admin password').fill('correct horse')
  await page.getByRole('button', { name: 'Log in' }).click()
  await initialResync

  // 3) After reload we land on /search (default route). Switch to Everywhere.
  await expect(page.getByRole('button', { name: 'Everywhere' })).toBeVisible()
  await page.getByRole('button', { name: 'Everywhere' }).click()

  // 4) Search; the SSE mock returns one not-in-library track.
  await page.getByPlaceholder('Search your library…').fill(externalTrack.title)
  await expect(page.getByText(externalTrack.title)).toBeVisible()

  // The Download button is present (row is NOT in library — GET /downloads was []).
  const downloadBtn = page.getByRole('button', { name: `Download ${externalTrack.title}` })
  await expect(downloadBtn).toBeVisible()

  // 5) Click Download -> POST /downloads -> queued job-1. The row shows a progress
  //    indicator. WAIT for that downloading state to render (so the POST's upsert
  //    has applied) BEFORE sending the WS completion frame — otherwise the POST
  //    response can resolve after the completion and clobber it back to queued.
  await downloadBtn.click()
  await expect(page.getByTitle(/^Downloading/)).toBeVisible()
  await ws.complete()

  // The Download/progress state disappears; the in-library ✓ row appears.
  await expect(downloadBtn).toHaveCount(0)
  await expect(page.getByTitle('In library')).toBeVisible()

  // 6) Play: clicking the in-library row plays the synthesized track. The player
  //    bar shows the title and a Pause button (playing flipped true). The
  //    one-shot SSE stream closes on completion (SearchStream closes on error),
  //    so the row is stable — a normal click works without reconnect churn.
  await page.getByRole('button', { name: new RegExp(externalTrack.title) }).first().click()
  await expect(page.getByTestId('player-bar').getByText(externalTrack.title)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible()
})
