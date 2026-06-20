import type { RealtimeEvent } from './types'

// WebSocketLike is the minimal slice we use so tests inject a stub (no real
// network/socket). The browser `WebSocket` satisfies it.
export interface WebSocketLike {
  onopen: (() => void) | null
  onmessage: ((ev: { data: string }) => void) | null
  onclose: (() => void) | null
  onerror: (() => void) | null
  close(): void
}

export interface RealtimeHandlers {
  onEvent(ev: RealtimeEvent): void
  onOpen?(): void
  onClose?(): void
}

const MAX_BACKOFF_MS = 15_000
const BASE_BACKOFF_MS = 1_000

// RealtimeConnection is the WebSocket transport for live download/library events.
// It is DISTINCT from the SSE SearchStream: it connects to /api/v1/ws (same-origin
// → the session cookie is sent automatically), reconnects with capped backoff,
// and resubscribes by reopening (the server re-subscribes all topics on connect).
// On (re)open, onOpen fires so the caller can resync (re-fetch GET /downloads).
export class RealtimeConnection {
  private socket: WebSocketLike | null = null
  private closedByUser = false
  private backoff = BASE_BACKOFF_MS
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  // Declared as plain fields (NOT constructor parameter properties): the tsconfig
  // sets erasableSyntaxOnly, which forbids `constructor(private x: ...)` shorthand.
  private readonly handlers: RealtimeHandlers
  private readonly makeSocket: (url: string) => WebSocketLike

  constructor(
    handlers: RealtimeHandlers,
    makeSocket?: (url: string) => WebSocketLike,
  ) {
    this.handlers = handlers
    this.makeSocket =
      makeSocket ??
      ((url) => new WebSocket(url) as unknown as WebSocketLike)
    this.connect()
  }

  private url(): string {
    // Same-origin ws(s):// URL derived from the page location.
    if (typeof location !== 'undefined') {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
      return `${proto}//${location.host}/api/v1/ws`
    }
    return 'ws://localhost/api/v1/ws'
  }

  private connect() {
    const s = this.makeSocket(this.url())
    this.socket = s
    s.onopen = () => {
      this.backoff = BASE_BACKOFF_MS // reset on a successful connect
      this.handlers.onOpen?.()
    }
    s.onmessage = (ev) => {
      try {
        const frame = JSON.parse(ev.data) as RealtimeEvent
        if (frame && typeof frame.type === 'string') {
          this.handlers.onEvent(frame)
        }
      } catch {
        // ignore malformed frame
      }
    }
    s.onclose = () => {
      this.handlers.onClose?.()
      if (!this.closedByUser) this.scheduleReconnect()
    }
    s.onerror = () => {
      // onclose follows; reconnect is handled there.
    }
  }

  private scheduleReconnect() {
    if (this.retryTimer) clearTimeout(this.retryTimer)
    const delay = this.backoff
    this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS)
    this.retryTimer = setTimeout(() => {
      if (!this.closedByUser) this.connect()
    }, delay)
  }

  close() {
    this.closedByUser = true
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.socket?.close()
  }
}
