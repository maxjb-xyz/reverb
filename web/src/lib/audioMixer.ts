import type { AudioElement } from './audioEngine'

// The Web Audio side of the player. Each audio element gets its own gain
// ("channel"), every channel feeds one music bus, and the bus feeds the
// speakers:
//
//   element -> channel gain -> music bus -> destination
//
// Channel gains carry crossfades; the music bus is lowered when the DJ talks
// over a song. Elements are only routed here when a feature needs it: once an
// element is wrapped by createMediaElementSource its sound only reaches the
// speakers through this graph, and the graph is silent while the browser keeps
// the audio context suspended.

export interface Mixer {
  /** Route an element through the mixer. False when it cannot be routed. Safe to repeat. */
  connect(el: AudioElement): boolean
  /** Set an element's channel gain now, cancelling any fade in progress. */
  setGain(el: AudioElement, v: number): void
  /** Fade an element's channel gain from `from` to `to` over `ms`, equal-power. */
  fade(el: AudioElement, from: number, to: number, ms: number): void
  /** Bring the whole music bus to `level` over `ms` (1 = normal). */
  duckMusic(level: number, ms: number): void
  /** Resume a suspended audio context. Call from a user gesture. */
  resume(): void
}

const CURVE_POINTS = 64

/**
 * Equal-power fade between two gains: the two halves of a crossfade sum to
 * constant loudness, so the middle does not dip the way a linear fade does.
 */
export function fadeCurve(from: number, to: number, points = CURVE_POINTS): Float32Array {
  const curve = new Float32Array(points)
  for (let i = 0; i < points; i++) {
    const t = points === 1 ? 1 : i / (points - 1)
    curve[i] =
      to >= from
        ? from + (to - from) * Math.sin((t * Math.PI) / 2)
        : to + (from - to) * Math.cos((t * Math.PI) / 2)
  }
  return curve
}

type AudioContextCtor = new () => AudioContext

function contextCtor(): AudioContextCtor | undefined {
  if (typeof globalThis === 'undefined') return undefined
  const g = globalThis as unknown as {
    AudioContext?: AudioContextCtor
    webkitAudioContext?: AudioContextCtor
  }
  return g.AudioContext ?? g.webkitAudioContext
}

/** A mixer over a real AudioContext, or null when the browser has no Web Audio. */
export function createWebAudioMixer(): Mixer | null {
  const Ctor = contextCtor()
  if (!Ctor) return null

  let ctx: AudioContext | null = null
  let music: GainNode | null = null
  const channels = new Map<AudioElement, GainNode>()

  // The context is made on first use, not at import, so nothing is created
  // for people who never turn a mixing feature on.
  function graph(): { ctx: AudioContext; music: GainNode } {
    if (!ctx || !music) {
      ctx = new Ctor!()
      music = ctx.createGain()
      music.connect(ctx.destination)
    }
    return { ctx, music }
  }

  function param(el: AudioElement): { p: AudioParam; now: number } | null {
    const ch = channels.get(el)
    if (!ch || !ctx) return null
    return { p: ch.gain, now: ctx.currentTime }
  }

  return {
    connect(el) {
      if (channels.has(el)) return true
      if (typeof HTMLMediaElement === 'undefined' || !(el instanceof HTMLMediaElement)) return false
      try {
        const g = graph()
        const source = g.ctx.createMediaElementSource(el)
        const channel = g.ctx.createGain()
        source.connect(channel)
        channel.connect(g.music)
        channels.set(el, channel)
        return true
      } catch {
        return false
      }
    },

    setGain(el, v) {
      const c = param(el)
      if (!c) return
      c.p.cancelScheduledValues(c.now)
      c.p.setValueAtTime(v, c.now)
    },

    fade(el, from, to, ms) {
      const c = param(el)
      if (!c) return
      c.p.cancelScheduledValues(c.now)
      const seconds = Math.max(ms, 1) / 1000
      try {
        c.p.setValueCurveAtTime(fadeCurve(from, to), c.now, seconds)
      } catch {
        // Some engines refuse a curve that touches another event; a straight
        // ramp is an acceptable fallback.
        c.p.setValueAtTime(from, c.now)
        c.p.linearRampToValueAtTime(to, c.now + seconds)
      }
    },

    duckMusic(level, ms) {
      if (!ctx || !music) return
      const now = ctx.currentTime
      music.gain.cancelScheduledValues(now)
      music.gain.setValueAtTime(music.gain.value, now)
      music.gain.linearRampToValueAtTime(level, now + Math.max(ms, 1) / 1000)
    },

    resume() {
      if (ctx && ctx.state !== 'running') void ctx.resume()
    },
  }
}
