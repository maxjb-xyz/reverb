import { test, expect } from '@playwright/test'
import { installApiMocks, installCompletenessMocks, installCompletenessWsMock } from './mocks'

// The completeness flow, end to end and hermetic:
//   artist page (coverage SSE → partial chip)
//   → open the partial album (1 of 2 in library, missing row has Download)
//   → download the missing track → WS complete → row flips to In Library (live)
//   → re-navigate proves the persistent album-level flip (2 of 2)
//   → a library playlist is reachable and plays.
test('completeness: artist coverage -> partial album -> download missing -> flips owned -> playlist plays', async ({ page }) => {
  const authed = { value: false }
  // Base mocks (me/setup/login/adapters/stream/cover + a default GET /downloads).
  await installApiMocks(page, authed)
  // Completeness routes (artist/coverage/album/playlist + a stateful /downloads that
  // wins over the base one since it is registered later). Returns a control to flip
  // the album mock to its all-owned state for the persistent-flip assertion.
  const completeness = await installCompletenessMocks(page)
  // WS mock for the MISSING-TRACK completion. Does NOT send any frame until complete().
  const ws = await installCompletenessWsMock(page)

  // 1) Load → not authed → Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()

  // 2) Log in. The app reloads; /me now returns authed. After the reload the realtime
  //    socket opens and resyncs the download list once (GET /downloads). Wait for that
  //    initial resync to settle BEFORE we enqueue so its (empty) result can't land late
  //    and clobber the completed job (mock-only race; mirrors core-loop).
  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)
  await page.getByPlaceholder('Admin password').fill('correct horse')
  await page.getByRole('button', { name: 'Log in' }).click()
  await initialResync

  // 3) Artist page. The album card renders AND the partial coverage chip ("1/2")
  //    appears — proving the coverage SSE stream drove the CoverageChip.
  await page.goto('/artist/spotify/art-1')
  await expect(page.getByRole('heading', { name: 'Mock Artist' })).toBeVisible()
  // exact:true → the album CARD button (aria-label "Mock Album"), not the hover
  // "Download Mock Album" overlay button the card also renders for a partial album.
  const albumCard = page.getByRole('button', { name: 'Mock Album', exact: true })
  await expect(albumCard).toBeVisible()
  await expect(page.getByText('1/2')).toBeVisible()

  // 4) Open the album. Both track titles render; the header shows "1 of 2 in library";
  //    the missing row "Missing Song" exposes a Download affordance.
  await albumCard.click()
  await expect(page).toHaveURL(/\/album\/spotify\/al-1$/)
  await expect(page.getByText('Owned Song', { exact: true })).toBeVisible()
  await expect(page.getByText('Missing Song', { exact: true })).toBeVisible()
  await expect(page.getByText('1 of 2 in library')).toBeVisible()

  // The per-row Download button for the missing track (aria-label "Download Missing Song",
  // exact so it doesn't match the header "Download missing · 1" button).
  const rowDownloadBtn = page.getByRole('button', { name: 'Download Missing Song', exact: true })
  await expect(rowDownloadBtn).toBeVisible()

  // 5) Headline live-flip: click the missing row's Download → POST /downloads →
  //    "Downloading". WAIT for that state to render (so the POST upsert has applied)
  //    BEFORE sending the WS completion frame (mirror core-loop ordering), else the
  //    POST response can resolve after completion and clobber it back to queued.
  await rowDownloadBtn.click()
  await expect(page.getByText('Downloading')).toBeVisible()
  await ws.complete()

  // The row's Download button disappears and the In-Library button appears — the
  // missing track is now owned (live, without re-navigation).
  await expect(rowDownloadBtn).toHaveCount(0)
  await expect(page.getByTitle('In Library')).toBeVisible()

  // 6) Persistent flip: the album mock now returns all-owned. Re-navigate (a full
  //    page load, so no stale React Query cache) and assert the album-level state
  //    flipped to complete: the missing track now renders but its per-row Download
  //    affordance is gone, the header "Download missing" CTA is gone, and the
  //    "N of M in library" partial banner is gone. (Scope to album-level affordances
  //    only — the shell's always-present "Downloads" tray toggle is unrelated.)
  completeness.setAlbumFull()
  await page.goto('/album/spotify/al-1')
  await expect(page.getByRole('heading', { name: 'Mock Album' })).toBeVisible()
  await expect(page.getByText('Missing Song', { exact: true })).toBeVisible()
  await expect(page.getByText('in library')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Download Missing Song', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^Download missing/ })).toHaveCount(0)

  // 7) Playlist: reachable and plays. "Chill Mix" + the owned track render; clicking
  //    Play starts the track — the player bar shows the title and Pause appears.
  await page.goto('/playlist/pl-1')
  await expect(page.getByRole('heading', { name: 'Chill Mix' })).toBeVisible()
  await expect(page.getByText('Owned Song', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Play Chill Mix' }).click()
  await expect(page.getByTestId('player-bar').getByText('Owned Song')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible()
})
