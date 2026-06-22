import type { Page, Route, WebSocketRoute } from '@playwright/test'

// One external track that is NOT in the library yet (match.status = not_in_library).
export const externalTrack = {
  source: 'spotify',
  externalId: 'ext-1',
  title: 'Test Anthem',
  artist: 'Mock Artist',
  album: 'Mock Album',
  durationMs: 200_000,
  isrc: 'TESTISRC0001',
  type: 'track' as const,
  match: { status: 'not_in_library' as const, libraryTrackId: '', method: 'none' as const, confidence: 0 },
}

// The library track id the completed download resolves to (flips the row).
export const libraryTrackId = 'lib-track-1'

// The queued job returned by POST /downloads.
function queuedJob() {
  return {
    id: 'job-1',
    dedupKey: `isrc:${externalTrack.isrc.toLowerCase()}`,
    status: 'queued',
    progress: 0,
    downloaderName: 'spotdl',
    priority: 0,
    attempts: 0,
    source: externalTrack.source,
    externalId: externalTrack.externalId,
    artist: externalTrack.artist,
    title: externalTrack.title,
    album: externalTrack.album,
    isrc: externalTrack.isrc,
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: 0,
    finishedAt: 0,
  }
}

// The completed job (after the WS download.complete frame) — resolves to a library track.
function completedJob() {
  return { ...queuedJob(), status: 'completed', progress: 100, libraryTrackId, finishedAt: Date.now() / 1000 }
}

// downloadState is the single source of truth for the mocked download list. GET
// /downloads serves it so the realtime onOpen RESYNC is always consistent with the
// POST + the WS completion. Without this, the one resync (getDownloads) can resolve
// AFTER ws.complete() and setAll([]) would wipe the completed job — flipping the row
// back out of the library mid-test. Mutated by POST /downloads and by ws.complete().
const downloadState: { jobs: ReturnType<typeof queuedJob>[] } = { jobs: [] }

// installApiMocks intercepts every /api/v1/* HTTP call. `authed` is a mutable box
// so the session flips to authenticated after login (the app calls
// /setup/status then /me on load, and reloads after POST /auth/login).
export async function installApiMocks(page: Page, authed: { value: boolean }) {
  downloadState.jobs = [] // reset per test
  await page.route('**/api/v1/setup/status', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ setupRequired: false }) }),
  )

  await page.route('**/api/v1/me', (route: Route) =>
    authed.value
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ authenticated: true }) })
      : route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'unauthorized' }) }),
  )

  await page.route('**/api/v1/auth/login', (route: Route) => {
    authed.value = true
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) })
  })

  // Everywhere search SSE. A finite text/event-stream body delivers the data: frame
  // to onmessage, then the stream ends. A browser EventSource treats that end as a
  // dropped connection and reconnects — and under Playwright's route.fulfill that
  // reconnect can happen in a tight loop, re-dispatching the envelope and churning
  // the result rows (detaching them mid-click). We serve the real envelope ONCE,
  // then answer every reconnect with HTTP 204, which per the EventSource spec tells
  // the client to STOP reconnecting — so the rows settle and stay clickable.
  // (Production correctness is handled separately: SearchStream.onerror closes the
  // one-shot stream so the real server's normal close never triggers a reconnect.)
  // Do NOT switch to a hanging body — onmessage would never fire under fulfill.
  let everywhereServed = false
  await page.route('**/api/v1/search/everywhere**', (route: Route) => {
    if (everywhereServed) {
      return route.fulfill({ status: 204, body: '' })
    }
    everywhereServed = true
    const envelope = { source: 'spotify', status: 'ok', results: [externalTrack] }
    const body = `data: ${JSON.stringify(envelope)}\n\n`
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body })
  })

  // Library search (library mode) — empty; the spec uses Everywhere mode.
  await page.route('**/api/v1/library/search**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tracks: [], albums: [], artists: [] }) }),
  )

  // /downloads is STATEFUL via downloadState so the realtime onOpen resync stays
  // consistent with the POST + WS completion. GET starts empty (row shows a visible
  // Download button). POST enqueues the queued job. ws.complete() later sets the
  // state to the completed job AND sends the WS frame, so even if the single resync
  // resolves after completion it returns [completed] — never wiping the flip.
  await page.route('**/api/v1/downloads', (route: Route) => {
    if (route.request().method() === 'POST') {
      downloadState.jobs = [queuedJob()]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(queuedJob()) })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(downloadState.jobs) })
  })

  // Adapters list — one enabled spotDL downloader so DownloadAction shows the
  // Download button (without this it falls through to the "No downloader" badge).
  await page.route('**/api/v1/adapters', (route: Route) => {
    if (route.request().method() === 'GET') {
      const adapters = [
        {
          id: 'spotdl-1',
          type: 'downloader',
          name: 'spotdl',
          enabled: true,
          priority: 0,
          config: {},
        },
      ]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(adapters) })
    }
    return route.continue()
  })

  // Stream proxy → tiny audio body so the <audio> src resolves (no real media).
  await page.route('**/api/v1/stream/**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'audio/mpeg', body: '' }),
  )

  // Cover proxy → 1x1 transparent png-ish bytes (never actually displayed in assertions).
  await page.route('**/api/v1/cover/**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'image/png', body: '' }),
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Completeness flow mocks (Task 19): artist coverage → partial album → download
// the one missing track → it flips owned → a library playlist plays.
//
// These live alongside the core-loop mocks but keep their OWN state (a separate
// download state + a stateful album detail) so they never touch the core-loop
// `downloadState`. installCompletenessMocks is registered AFTER installApiMocks,
// and Playwright matches routes most-recently-registered-first, so the
// completeness `**/api/v1/downloads` handler wins for this spec.
// ─────────────────────────────────────────────────────────────────────────────

// The library track id the completed missing-track download resolves to.
export const missingLibraryTrackId = 'lib-track-miss-1'

// The owned track already in the library (track 1 of the album / playlist track).
function ownedLibraryTrack() {
  return {
    id: 'lib-owned-1',
    title: 'Owned Song',
    artist: 'Mock Artist',
    durationMs: 200_000,
    album: 'Mock Album',
    albumId: 'al-1',
    artistId: 'art-1',
    coverArtId: '',
    trackNumber: 1,
    discNumber: 1,
    bitRate: 0,
    suffix: '',
    contentType: '',
  }
}

// ArtistDetail with one album (partial coverage delivered via the SSE stream).
function artistDetail() {
  return {
    source: 'spotify',
    id: 'art-1',
    name: 'Mock Artist',
    resolved: true,
    albums: [
      {
        source: 'spotify',
        externalId: 'al-1',
        name: 'Mock Album',
        year: 2020,
        kind: 'album',
        totalTracks: 2,
        coverUrl: '',
      },
    ],
  }
}

// The partial AlbumDetail: track 1 owned (full), track 2 missing (none).
function albumDetailPartial() {
  return {
    source: 'spotify',
    id: 'al-1',
    name: 'Mock Album',
    artist: 'Mock Artist',
    year: 2020,
    totalCount: 2,
    ownedCount: 1,
    tracks: [
      {
        state: 'full',
        libraryTrack: ownedLibraryTrack(),
        title: 'Owned Song',
        artist: 'Mock Artist',
        trackNumber: 1,
        durationMs: 200_000,
      },
      {
        state: 'none',
        externalRef: {
          source: 'spotify',
          externalId: 'ext-miss-1',
          title: 'Missing Song',
          durationMs: 180_000,
        },
        title: 'Missing Song',
        artist: 'Mock Artist',
        trackNumber: 2,
        durationMs: 180_000,
      },
    ],
  }
}

// The full AlbumDetail after the missing track has been downloaded: BOTH tracks
// owned. Re-navigating to the album returns this — proving the persistent flip.
function albumDetailFull() {
  return {
    source: 'spotify',
    id: 'al-1',
    name: 'Mock Album',
    artist: 'Mock Artist',
    year: 2020,
    totalCount: 2,
    ownedCount: 2,
    tracks: [
      {
        state: 'full',
        libraryTrack: ownedLibraryTrack(),
        title: 'Owned Song',
        artist: 'Mock Artist',
        trackNumber: 1,
        durationMs: 200_000,
      },
      {
        state: 'full',
        libraryTrack: {
          id: missingLibraryTrackId,
          title: 'Missing Song',
          artist: 'Mock Artist',
          durationMs: 180_000,
          album: 'Mock Album',
          albumId: 'al-1',
          artistId: 'art-1',
          coverArtId: '',
          trackNumber: 2,
          discNumber: 1,
          bitRate: 0,
          suffix: '',
          contentType: '',
        },
        title: 'Missing Song',
        artist: 'Mock Artist',
        trackNumber: 2,
        durationMs: 180_000,
      },
    ],
  }
}

// The queued / completed missing-track download jobs (own externalId so the album
// row's DownloadAction.byExternal('spotify','ext-miss-1') matches it).
function missingQueuedJob() {
  return {
    id: 'job-miss-1',
    dedupKey: 'ext:spotify:ext-miss-1',
    status: 'queued',
    progress: 0,
    downloaderName: 'spotdl',
    priority: 0,
    attempts: 0,
    source: 'spotify',
    externalId: 'ext-miss-1',
    artist: 'Mock Artist',
    title: 'Missing Song',
    album: 'Mock Album',
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: 0,
    finishedAt: 0,
  }
}

function missingCompletedJob() {
  return {
    ...missingQueuedJob(),
    status: 'completed',
    progress: 100,
    libraryTrackId: missingLibraryTrackId,
    finishedAt: Date.now() / 1000,
  }
}

// CompletenessControl lets the spec flip the album mock to its all-owned state
// (for the persistent re-navigation assertion).
export type CompletenessControl = { setAlbumFull: () => void }

// Per-test state for the completeness flow, isolated from the core-loop
// `downloadState`.
type CompletenessState = {
  albumFull: boolean
  downloads: ReturnType<typeof missingQueuedJob>[]
}

export async function installCompletenessMocks(page: Page): Promise<CompletenessControl> {
  const state: CompletenessState = { albumFull: false, downloads: [] }

  // ArtistDetail.
  await page.route('**/api/v1/artist/spotify/art-1', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(artistDetail()) }),
  )

  // Coverage SSE — served-once-then-204 (mirrors the everywhere search mock): one
  // partial coverage frame for al-1, then 204 on every EventSource reconnect so the
  // browser stops reconnecting and the chip settles. (CoverageStream.onerror also
  // closes the one-shot stream in production.)
  let coverageServed = false
  await page.route('**/api/v1/artist/spotify/art-1/coverage', (route: Route) => {
    if (coverageServed) return route.fulfill({ status: 204, body: '' })
    coverageServed = true
    const frame = {
      source: 'spotify',
      externalAlbumId: 'al-1',
      state: 'partial',
      ownedCount: 1,
      totalCount: 2,
      libraryAlbumId: '',
      missingTracks: [
        { source: 'spotify', externalId: 'ext-miss-1', title: 'Missing Song', durationMs: 180_000 },
      ],
    }
    const body = `data: ${JSON.stringify(frame)}\n\n`
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body })
  })

  // AlbumDetail — STATEFUL: partial until setAlbumFull() flips it to all-owned.
  await page.route('**/api/v1/album/spotify/al-1', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(state.albumFull ? albumDetailFull() : albumDetailPartial()),
    }),
  )

  // /downloads — STATEFUL for the missing-track job (own state; does NOT touch the
  // core-loop downloadState). GET starts empty (row shows Download). POST enqueues
  // the queued missing job. completeMissing() (WS) flips it to completed so the
  // polling-fallback resync / onOpen resync stay consistent.
  await page.route('**/api/v1/downloads', (route: Route) => {
    if (route.request().method() === 'POST') {
      state.downloads = [missingQueuedJob()]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(missingQueuedJob()) })
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(state.downloads) })
  })

  // Library playlist.
  await page.route('**/api/v1/library/playlist/pl-1', (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'pl-1',
        name: 'Chill Mix',
        coverArtId: '',
        songCount: 1,
        durationMs: 200_000,
        tracks: [ownedLibraryTrack()],
      }),
    }),
  )

  // Expose the completion state to the WS trigger via a shared closure module-level
  // ref so installCompletenessWsMock can flip `state.downloads`.
  completenessStateRef = state
  return { setAlbumFull: () => { state.albumFull = true } }
}

// Module-level ref so the completeness WS trigger can reflect the completion in the
// completeness download state (mirrors how installWsMock reflects into downloadState).
let completenessStateRef: CompletenessState | null = null

// installCompletenessWsMock mirrors installWsMock but fires the completion frame for
// the MISSING-TRACK job (job-miss-1 / ext-miss-1 → missingLibraryTrackId), and
// reflects it into the completeness download state.
export async function installCompletenessWsMock(page: Page): Promise<WsTrigger> {
  let capturedWs: WebSocketRoute | null = null

  await page.routeWebSocket('**/api/v1/ws', (ws: WebSocketRoute) => {
    capturedWs = ws
  })

  return {
    complete: () =>
      new Promise<void>((resolve, reject) => {
        const deadline = Date.now() + 5000
        const poll = () => {
          if (capturedWs) {
            if (completenessStateRef) completenessStateRef.downloads = [missingCompletedJob()]
            const frame = {
              type: 'download.complete',
              payload: {
                jobId: 'job-miss-1',
                dedupKey: 'ext:spotify:ext-miss-1',
                status: 'completed',
                progress: 100,
                source: 'spotify',
                externalId: 'ext-miss-1',
                libraryTrackId: missingLibraryTrackId,
              },
            }
            capturedWs.send(JSON.stringify(frame))
            resolve()
          } else if (Date.now() > deadline) {
            reject(new Error('installCompletenessWsMock: WebSocket never opened within 5 s'))
          } else {
            setTimeout(poll, 20)
          }
        }
        poll()
      }),
  }
}

// WsTrigger lets the spec fire the completion frame at the right moment.
export type WsTrigger = { complete: () => Promise<void> }

// installWsMock intercepts the realtime WebSocket. On connect it does NOT send any
// frame; instead it captures the WebSocketRoute and returns a WsTrigger. The spec
// calls await ws.complete() ONLY AFTER clicking the Download button (which fires
// POST /downloads and upserts the queued job-1). complete() sends the
// download.complete frame so applyEvent flips job-1 to completed+libraryTrackId
// and the row becomes in-library. page.routeWebSocket is async — always await it.
export async function installWsMock(page: Page): Promise<WsTrigger> {
  let capturedWs: WebSocketRoute | null = null

  await page.routeWebSocket('**/api/v1/ws', (ws: WebSocketRoute) => {
    // Fully mocked — do not connectToServer(). Capture for later use.
    capturedWs = ws
  })

  return {
    complete: () =>
      new Promise<void>((resolve, reject) => {
        const deadline = Date.now() + 5000
        const poll = () => {
          if (capturedWs) {
            // Reflect the completion in the resync source too, so any in-flight or
            // later GET /downloads returns the completed job rather than wiping it.
            downloadState.jobs = [completedJob()]
            const frame = {
              type: 'download.complete',
              payload: {
                jobId: 'job-1',
                dedupKey: `isrc:${externalTrack.isrc.toLowerCase()}`,
                status: 'completed',
                progress: 100,
                source: externalTrack.source,
                externalId: externalTrack.externalId,
                libraryTrackId,
              },
            }
            capturedWs.send(JSON.stringify(frame))
            resolve()
          } else if (Date.now() > deadline) {
            reject(new Error('installWsMock: WebSocket never opened within 5 s'))
          } else {
            setTimeout(poll, 20)
          }
        }
        poll()
      }),
  }
}
