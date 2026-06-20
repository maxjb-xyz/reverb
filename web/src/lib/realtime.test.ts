import { describe, it, expect, vi } from 'vitest'
import { RealtimeConnection, type WebSocketLike } from './realtime'

// stubSocket captures handlers so the test drives open/message/close.
function makeStub() {
  const sockets: StubSocket[] = []
  class StubSocket implements WebSocketLike {
    onopen: (() => void) | null = null
    onmessage: ((ev: { data: string }) => void) | null = null
    onclose: (() => void) | null = null
    onerror: (() => void) | null = null
    closed = false
    url: string
    constructor(url: string) {
      this.url = url
      sockets.push(this)
    }
    close() {
      this.closed = true
      this.onclose?.()
    }
  }
  return { sockets, StubSocket }
}

describe('RealtimeConnection', () => {
  it('dispatches typed frames and calls onOpen', () => {
    const { sockets, StubSocket } = makeStub()
    const events: { type: string }[] = []
    let opened = 0
    const conn = new RealtimeConnection(
      { onEvent: (ev) => events.push(ev), onOpen: () => opened++ },
      (url) => new StubSocket(url),
    )
    const s = sockets[0]
    expect(s.url).toContain('/api/v1/ws')

    s.onopen?.()
    expect(opened).toBe(1)

    s.onmessage?.({ data: JSON.stringify({ type: 'download.progress', payload: { jobId: 'j1', progress: 42 } }) })
    expect(events).toHaveLength(1)
    expect(events[0].type).toBe('download.progress')

    // Malformed frame is ignored (no throw).
    s.onmessage?.({ data: 'not json' })
    expect(events).toHaveLength(1)

    conn.close()
    expect(s.closed).toBe(true)
  })

  it('reconnects with backoff after an unexpected close', () => {
    vi.useFakeTimers()
    const { sockets, StubSocket } = makeStub()
    const conn = new RealtimeConnection({ onEvent: () => {} }, (url) => new StubSocket(url))
    // Simulate the socket dropping.
    sockets[0].onclose?.()
    // After the first backoff delay, a new socket is created.
    vi.advanceTimersByTime(1000)
    expect(sockets.length).toBe(2)
    conn.close()
    vi.useRealTimers()
  })
})
