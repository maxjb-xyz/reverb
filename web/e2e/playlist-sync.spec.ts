import { test, expect } from '@playwright/test'
import { installApiMocks, installPlaylistSyncMocks, installPlaylistSyncWsMock } from './mocks'

// The playlist-sync flow, end to end and hermetic:
//   login → Library (Playlists tab) → "Import from Spotify" dialog → paste URL →
//   Import → land on /synced-playlist/sp1 ("My Mix", 1 of 2 in library, missing row
//   has Download) → download the missing track → WS complete → row flips In Library
//   (live) → "Sync now" → an added 3rd track appears.
test('playlist sync: import -> have/missing -> download missing -> flips owned -> sync surfaces an added track', async ({ page }) => {
  const authed = { value: false }
  // Base mocks (me/setup/login/adapters/stream/cover + a default GET /downloads).
  await installApiMocks(page, authed)
  // Playlist-sync routes (synced-playlists CRUD + a stateful /downloads + empty
  // library lists). Registered AFTER installApiMocks so its handlers win. Owns its
  // OWN isolated state — never touches the core-loop/completeness download state.
  await installPlaylistSyncMocks(page)
  // WS mock for the MISSING-TRACK completion. Sends no frame until complete().
  const ws = await installPlaylistSyncWsMock(page)

  // 1) Load → not authed → Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome back' })).toBeVisible()

  // 2) Log in. The app reloads; /me now returns authed. After the reload the realtime
  //    socket opens and resyncs the download list once (GET /downloads). Wait for that
  //    initial resync to settle BEFORE we enqueue so its (empty) result can't land
  //    late and clobber the completed job (mock-only race; mirrors completeness).
  const initialResync = page
    .waitForResponse((r) => r.url().includes('/api/v1/downloads') && r.request().method() === 'GET')
    .catch(() => undefined)
  await page.getByPlaceholder('Admin password').fill('correct horse')
  await page.getByRole('button', { name: 'Log in' }).click()
  await initialResync

  // 3) Library page → Playlists tab → "Import from Spotify" opens the dialog.
  await page.goto('/library')
  await expect(page.getByRole('heading', { name: 'Your Library' })).toBeVisible()
  // Scope to the page's filter group (a "Playlists" button also exists in the
  // LibraryRail aside — getByRole alone is a strict-mode collision).
  await page.getByRole('group', { name: 'Library filter' }).getByRole('button', { name: 'Playlists' }).click()
  await page.getByRole('main').getByRole('button', { name: 'Import from Spotify' }).click()

  // The import dialog opens. Paste a playlist URL and click Import.
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Import from Spotify' })).toBeVisible()
  await dialog.getByLabel('Playlist URL').fill('https://open.spotify.com/playlist/PL')
  await dialog.getByRole('button', { name: 'Import', exact: true }).click()

  // 4) Lands on the synced playlist page: "My Mix" + "1 of 2 in library". Both track
  //    titles render; the missing row exposes a Download affordance.
  await expect(page).toHaveURL(/\/playlist\/sp1$/)
  await expect(page.getByRole('heading', { name: 'My Mix' })).toBeVisible()
  await expect(page.getByText('1 of 2 in library')).toBeVisible()
  await expect(page.getByText('Synced Owned Song', { exact: true })).toBeVisible()
  await expect(page.getByText('Synced Missing Song', { exact: true })).toBeVisible()

  // The per-row Download button for the missing track (aria-label "Download Synced
  // Missing Song", exact so it doesn't match the header "Download all missing" CTA).
  const rowDownloadBtn = page.getByRole('button', { name: 'Download Synced Missing Song', exact: true })
  await expect(rowDownloadBtn).toBeVisible()

  // 5) Headline live-flip: click the missing row's Download → POST /downloads →
  //    "Queued". WAIT for that state to render (so the POST upsert has applied)
  //    BEFORE sending the WS completion frame (mirror core-loop/completeness
  //    ordering), else the POST response can resolve after completion and clobber it.
  await rowDownloadBtn.click()
  await expect(page.getByText('Queued')).toBeVisible()
  await ws.complete()

  // The row's Download button disappears — the missing track is now owned (live,
  // without re-navigation). Fix 6b invalidates the ['synced-playlist'] query on
  // download.complete, so the detail refetches and the row flips to a full TrackRow
  // (no DownloadAction, no "In Library" badge — just a playable track row).
  await expect(rowDownloadBtn).toHaveCount(0)
  // The track title is still visible as a normal (owned) row.
  await expect(page.getByText('Synced Missing Song', { exact: true })).toBeVisible()

  // 6) "Sync now" → the mock flips its `synced` flag; the page invalidates and
  //    re-fetches GET /synced-playlists/sp1, which now includes the added 3rd track.
  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect(page.getByText('Synced Added Song', { exact: true })).toBeVisible()
})
