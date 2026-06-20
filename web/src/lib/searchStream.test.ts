import { describe, expect, it, vi } from 'vitest'
import { SearchStream } from './searchStream'
import type { SearchEnvelope } from './types'

// stubSource lets the test fire messages/errors synchronously; records the URL + close.
class StubSource {
  onmessage: ((ev: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  url: string
  constructor(url: string) { this.url = url }
  close() {
    this.closed = true
  }
  emit(env: SearchEnvelope) {
    this.onmessage?.({ data: JSON.stringify(env) })
  }
}

describe('SearchStream', () => {
  it('opens a same-origin URL with q and type, and forwards envelopes', () => {
    let made: StubSource | null = null
    const got: SearchEnvelope[] = []
    const ss = new SearchStream('hello world', 'track', { onEnvelope: (e) => got.push(e) }, (url) => {
      made = new StubSource(url)
      return made
    })
    expect(made).not.toBeNull()
    expect(made!.url).toBe('/api/v1/search/everywhere?q=hello%20world&type=track')

    made!.emit({ source: 'spotify', status: 'ok', results: [] })
    expect(got).toHaveLength(1)
    expect(got[0].source).toBe('spotify')

    ss.close()
    expect(made!.closed).toBe(true)
  })

  it('calls onError on stream error', () => {
    const onError = vi.fn()
    let made: StubSource | null = null
    new SearchStream('q', 'track', { onEnvelope: () => {}, onError }, (url) => {
      made = new StubSource(url)
      return made
    })
    made!.onerror?.()
    expect(onError).toHaveBeenCalled()
  })
})
