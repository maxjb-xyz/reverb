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
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()

  // 2) Log in (username + password). On success the app re-navigates to the shell;
  //    the realtime socket opens and resyncs the download list once (GET /downloads).
  //    Wait for that initial resync to settle BEFORE we enqueue, so its (empty)
  //    result can't land late and clobber the completed job (mock-only race).
  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)
  await page.getByLabel('Username').fill('owner')
  await page.getByLabel('Password').fill('correct horse')
  const navigation = page.waitForNavigation({ waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'Log in' }).click()
  await navigation
  await expect(page.getByTestId('app-shell-root')).toBeVisible()
  await initialResync

  // 3) After reload we land on / (Home). Drive the search from the persistent
  //    TopBar input (the desktop search bar); Enter opens the full /search page.
  const topSearch = page.getByPlaceholder(/or everywhere/)
  await topSearch.fill(externalTrack.title)
  await topSearch.press('Enter')

  // 4) Results are blended: library rows first, then external rows stream in
  //    automatically ~400ms after typing (debounced SSE). The SSE mock returns
  //    one not-in-library track; rely on auto-waiting for it to appear.
  // exact:true so we match the track row's title span, not the "Results for
  // \"Test Anthem\"" results header (which also contains the title).
  await expect(page.getByText(externalTrack.title, { exact: true })).toBeVisible()

  // The Download button is present (row is NOT in library — GET /downloads was []).
  // Use exact: true to avoid matching the TrackRow's full accessible name which also
  // contains the track title.
  const downloadBtn = page.getByRole('button', { name: `Download ${externalTrack.title}`, exact: true })
  await expect(downloadBtn).toBeVisible()

  // 5) Click Download -> POST /downloads -> queued job-1. The row shows a
  //    "Queued" badge (no title attribute). WAIT for that queued state to
  //    render (so the POST's upsert has applied) BEFORE sending the WS completion
  //    frame — otherwise the POST response can resolve after the completion and
  //    clobber it back to queued.
  await downloadBtn.click()
  await expect(page.getByText('Queued')).toBeVisible()
  await ws.complete()

  // The Download/progress state disappears; the in-library button appears.
  // DownloadAction renders title="In Library" (capital L) on the in-library button.
  await expect(downloadBtn).toHaveCount(0)
  await expect(page.getByTitle('In Library')).toBeVisible()

  // 6) Play: clicking the in-library button (title="In Library") plays the
  //    synthesized track. The player bar (data-testid="player-bar") shows the title
  //    and the play/pause button becomes aria-label="Pause" (playing flipped true).
  //    Use title="In Library" to distinguish the DownloadAction play button from the
  //    TrackRow hover-play button (both share the same aria-label since Spotify semantics).
  await page.getByTitle('In Library').click()
  await expect(page.getByTestId('player-bar').getByText(externalTrack.title)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible()
})
