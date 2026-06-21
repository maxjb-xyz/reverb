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

// installApiMocks intercepts every /api/v1/* HTTP call. `authed` is a mutable box
// so the session flips to authenticated after login (the app calls
// /setup/status then /me on load, and reloads after POST /auth/login).
export async function installApiMocks(page: Page, authed: { value: boolean }) {
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

  // Everywhere search: a finite SSE body delivers the data: frame to onmessage then
  // closes; EventSource auto-reconnects and the persistent route re-fulfills the same
  // body — harmless because everywhereStore.appendSection dedups by dedupKey. Do NOT
  // "fix" this by switching to a hanging body: that would make onmessage never fire.
  await page.route('**/api/v1/search/everywhere**', (route: Route) => {
    const envelope = { source: 'spotify', status: 'ok', results: [externalTrack] }
    const body = `data: ${JSON.stringify(envelope)}\n\n`
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body })
  })

  // Library search (library mode) — empty; the spec uses Everywhere mode.
  await page.route('**/api/v1/library/search**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tracks: [], albums: [], artists: [] }) }),
  )

  // GET /downloads: return EMPTY array so the result row starts with a visible
  // Download button (not pre-completed). POST /downloads enqueues and returns the
  // queued job; the WS completion frame (sent by ws.complete() after the click)
  // then flips the row to in-library.
  await page.route('**/api/v1/downloads', (route: Route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(queuedJob()) })
    }
    // GET /downloads (WS onOpen resync) → empty: no pre-existing jobs.
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
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
