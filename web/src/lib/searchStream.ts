import type { SearchEnvelope } from './types'

// EventSourceLike is the minimal slice of EventSource we use, so tests inject a
// stub and no real network connection is opened.
export interface EventSourceLike {
  onmessage: ((ev: { data: string }) => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface SearchStreamHandlers {
  onEnvelope(e: SearchEnvelope): void
  onError?(): void
}

// SearchStream is the SSE transport for Everywhere search. It is DISTINCT from
// the REST fetch wrapper and the (future) WebSocket: EventSource hits the
// same-origin endpoint and carries the session cookie automatically.
export class SearchStream {
  private source: EventSourceLike

  constructor(
    q: string,
    type: 'track' | 'album' | 'artist',
    handlers: SearchStreamHandlers,
    makeSource: (url: string) => EventSourceLike = (url) => new EventSource(url, { withCredentials: true }) as unknown as EventSourceLike,
  ) {
    const url = `/api/v1/search/everywhere?q=${encodeURIComponent(q)}&type=${type}`
    this.source = makeSource(url)
    this.source.onmessage = (ev) => {
      try {
        handlers.onEnvelope(JSON.parse(ev.data) as SearchEnvelope)
      } catch {
        // ignore malformed event
      }
    }
    this.source.onerror = () => {
      handlers.onError?.()
    }
  }

  close() {
    this.source.close()
  }
}
