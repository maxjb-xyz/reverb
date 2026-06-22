import type { AlbumCoverage } from './types'

// EventSourceLike is the minimal slice of EventSource we use, so tests inject a
// stub and no real network connection is opened.
export interface EventSourceLike {
  onmessage: ((ev: { data: string }) => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface CoverageStreamHandlers {
  onCoverage(c: AlbumCoverage): void
  onError?(): void
}

// CoverageStream is the SSE transport for artist coverage. It is a ONE-SHOT
// stream: the server emits one AlbumCoverage frame per album, then closes the
// connection. A browser EventSource treats that close as a dropped connection
// and auto-reconnects, so we close ourselves on error to prevent re-running
// the coverage query in a loop.
export class CoverageStream {
  private source: EventSourceLike

  constructor(
    source: string,
    id: string,
    handlers: CoverageStreamHandlers,
    makeSource: (url: string) => EventSourceLike = (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
  ) {
    const url = `/api/v1/artist/${encodeURIComponent(source)}/${encodeURIComponent(id)}/coverage`
    this.source = makeSource(url)
    this.source.onmessage = (ev) => {
      try {
        handlers.onCoverage(JSON.parse(ev.data) as AlbumCoverage)
      } catch {
        // ignore malformed event
      }
    }
    this.source.onerror = () => {
      // ONE-SHOT stream: close to prevent the browser's auto-reconnect loop.
      this.source.close()
      handlers.onError?.()
    }
  }

  close() {
    this.source.close()
  }
}
