import { afterEach, describe, expect, it, vi } from 'vitest'
import { createWebAudioMixer, fadeCurve } from './audioMixer'

// A recording stand-in for the parts of the Web Audio API the mixer touches.
class FakeParam {
  value = 1
  events: Array<[string, ...unknown[]]> = []
  cancelScheduledValues(t: number) {
    this.events.push(['cancel', t])
  }
  setValueAtTime(v: number, t: number) {
    this.value = v
    this.events.push(['set', v, t])
  }
  setValueCurveAtTime(curve: Float32Array, t: number, d: number) {
    this.value = curve[curve.length - 1]
    this.events.push(['curve', Array.from(curve), t, d])
  }
  linearRampToValueAtTime(v: number, t: number) {
    this.value = v
    this.events.push(['ramp', v, t])
  }
}
class FakeNode {
  out: unknown[] = []
  connect(n: unknown) {
    this.out.push(n)
  }
}
class FakeGain extends FakeNode {
  gain = new FakeParam()
}
class FakeContext {
  static made: FakeContext[] = []
  state = 'suspended'
  currentTime = 10
  destination = new FakeNode()
  gains: FakeGain[] = []
  sources: Array<{ el: unknown; node: FakeNode }> = []
  resumed = 0
  constructor() {
    FakeContext.made.push(this)
  }
  createGain() {
    const g = new FakeGain()
    this.gains.push(g)
    return g
  }
  createMediaElementSource(el: unknown) {
    const node = new FakeNode()
    this.sources.push({ el, node })
    return node
  }
  resume() {
    this.resumed++
    this.state = 'running'
    return Promise.resolve()
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  FakeContext.made = []
})

describe('fadeCurve', () => {
  it('fades in along an equal-power curve', () => {
    const c = fadeCurve(0, 1, 5)
    expect(c[0]).toBeCloseTo(0)
    expect(c[2]).toBeCloseTo(Math.SQRT1_2) // half way = -3 dB, not -6 dB
    expect(c[4]).toBeCloseTo(1)
  })

  it('fades out along the mirrored curve', () => {
    const c = fadeCurve(1, 0, 5)
    expect(c[0]).toBeCloseTo(1)
    expect(c[2]).toBeCloseTo(Math.SQRT1_2)
    expect(c[4]).toBeCloseTo(0)
  })

  it('keeps both ends exact for partial ranges', () => {
    const c = fadeCurve(0.25, 1, 9)
    expect(c[0]).toBeCloseTo(0.25)
    expect(c[8]).toBeCloseTo(1)
  })
})

describe('createWebAudioMixer', () => {
  it('returns null when the browser has no Web Audio', () => {
    vi.stubGlobal('AudioContext', undefined)
    vi.stubGlobal('webkitAudioContext', undefined)
    expect(createWebAudioMixer()).toBeNull()
  })

  it('creates no audio context until something is connected', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    createWebAudioMixer()
    expect(FakeContext.made).toHaveLength(0)
  })

  it('routes an element through its own gain into the music bus, once', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    const el = new Audio()
    expect(m.connect(el)).toBe(true)
    expect(m.connect(el)).toBe(true) // idempotent: a media element can only be wrapped once
    const ctx = FakeContext.made[0]
    expect(ctx.sources).toHaveLength(1)
    const [music, channel] = ctx.gains
    expect(ctx.sources[0].node.out).toEqual([channel])
    expect(channel.out).toEqual([music])
    expect(music.out).toEqual([ctx.destination])
  })

  it('refuses things that are not media elements', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    expect(m.connect({ src: '' } as never)).toBe(false)
  })

  it('fades a channel with an equal-power curve starting now', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    const el = new Audio()
    m.connect(el)
    const channel = FakeContext.made[0].gains[1]
    m.fade(el, 0, 1, 4000)
    const curve = channel.gain.events.find((e) => e[0] === 'curve')!
    expect(curve[2]).toBe(10) // ctx.currentTime
    expect(curve[3]).toBe(4) // seconds
    expect(channel.gain.value).toBeCloseTo(1)
  })

  it('sets a channel gain immediately, cancelling any fade', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    const el = new Audio()
    m.connect(el)
    const channel = FakeContext.made[0].gains[1]
    m.setGain(el, 0.5)
    expect(channel.gain.events[0][0]).toBe('cancel')
    expect(channel.gain.value).toBe(0.5)
  })

  it('ducks and restores the whole music bus', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    m.connect(new Audio())
    const music = FakeContext.made[0].gains[0]
    m.duckMusic(0.25, 300)
    expect(music.gain.value).toBe(0.25)
    m.duckMusic(1, 300)
    expect(music.gain.value).toBe(1)
  })

  it('resumes a suspended context', () => {
    vi.stubGlobal('AudioContext', FakeContext)
    const m = createWebAudioMixer()!
    m.connect(new Audio())
    m.resume()
    expect(FakeContext.made[0].resumed).toBe(1)
    m.resume() // already running: nothing to do
    expect(FakeContext.made[0].resumed).toBe(1)
  })
})
