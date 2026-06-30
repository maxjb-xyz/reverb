import type { Track } from './types'
import { streamUrl } from './libraryApi'

export type RepeatMode = 'off' | 'all' | 'one'

export interface AudioElement {
  src: string
  currentTime: number
  duration: number
  volume: number
  paused: boolean
  play(): Promise<void>
  pause(): void
  load(): void
  buffered: { length: number; end(i: number): number; start(i: number): number }
  addEventListener(type: string, cb: () => void): void
  removeEventListener(type: string, cb: () => void): void
}

export interface PlayerState {
  queue: Track[]
  index: number
  current: Track | null
  playing: boolean
  currentTimeMs: number
  durationMs: number
  bufferedMs: number
  volume: number
  shuffle: boolean
  repeat: RepeatMode
}

function realAudioFactory(): AudioElement {
  return new Audio() as unknown as AudioElement
}

export class AudioEngine {
  private factory: () => AudioElement
  private resolveSrc: (t: Track) => string
  private active: AudioElement
  private preload: AudioElement
  private listeners = new Set<(s: PlayerState) => void>()

  private queue: Track[] = []
  private index = -1
  private playing = false
  private currentTimeMs = 0
  private durationMs = 0
  private bufferedMs = 0
  private volume = 1
  private shuffle = false
  private repeat: RepeatMode = 'off'

  // shuffle order: a permutation of queue indices; shufflePos points into it.
  private shuffleOrder: number[] = []
  private shufflePos = -1

  // stream-error recovery
  private consecutiveErrors = 0
  private repeatOneReloadAttempted = false

  constructor(
    factory: () => AudioElement = realAudioFactory,
    resolveSrc: (t: Track) => string = (t) => streamUrl(t.id),
  ) {
    this.factory = factory
    this.resolveSrc = resolveSrc
    this.active = this.factory()
    this.preload = this.factory()
    this.bindActive()
  }

  private bindActive() {
    this.active.addEventListener('timeupdate', this.onTime)
    this.active.addEventListener('durationchange', this.onTime)
    this.active.addEventListener('progress', this.onTime)
    this.active.addEventListener('ended', this.onEnded)
    this.active.addEventListener('play', this.onPlayState)
    this.active.addEventListener('pause', this.onPlayState)
    this.active.addEventListener('error', this.onError)
    // Note: preload errors are intentionally not handled — a preload error should
    // null/ignore the preload src silently, never advance the queue.
  }

  private onTime = () => {
    this.currentTimeMs = Math.round((this.active.currentTime || 0) * 1000)
    this.durationMs = Number.isFinite(this.active.duration)
      ? Math.round((this.active.duration || 0) * 1000)
      : this.durationMs
    const b = this.active.buffered
    if (b && b.length > 0) {
      this.bufferedMs = Math.round(b.end(b.length - 1) * 1000)
    }
    this.emit()
  }

  private onPlayState = () => {
    this.playing = !this.active.paused
    if (!this.active.paused) {
      // Successful play: reset error counters so isolated dead tracks don't accumulate
      this.consecutiveErrors = 0
      this.repeatOneReloadAttempted = false
    }
    this.emit()
  }

  private onError = () => {
    if (this.repeat === 'one') {
      // Attempt ONE reload on the pinned track. If the reload itself fires another error,
      // stop — never skip off the pinned track under repeat-one.
      if (!this.repeatOneReloadAttempted) {
        this.repeatOneReloadAttempted = true
        this.active.currentTime = 0
        void this.active.play()
        // playing stays true; let the next error (if any) fall through to the stop branch
        return
      }
      // Second failure: stop, do not advance
      this.playing = false
      this.emit()
      return
    }

    this.consecutiveErrors++
    if (this.consecutiveErrors >= 3) {
      // Backend-down storm: stop to prevent infinite skip loop
      this.playing = false
      this.consecutiveErrors = 0
      this.emit()
      return
    }

    // Skip the dead track and autoplay the next one
    this.advance(1, true)
  }

  private onEnded = () => {
    if (this.repeat === 'one') {
      this.active.currentTime = 0
      void this.active.play()
      this.playing = true
      this.emit()
      return
    }
    this.advance(1, true)
  }

  subscribe(cb: (s: PlayerState) => void): () => void {
    this.listeners.add(cb)
    cb(this.getState())
    return () => this.listeners.delete(cb)
  }

  getState(): PlayerState {
    return {
      queue: [...this.queue],
      index: this.index,
      current: this.index >= 0 && this.index < this.queue.length ? this.queue[this.index] : null,
      playing: this.playing,
      currentTimeMs: this.currentTimeMs,
      durationMs: this.durationMs,
      bufferedMs: this.bufferedMs,
      volume: this.volume,
      shuffle: this.shuffle,
      repeat: this.repeat,
    }
  }

  private emit() {
    const s = this.getState()
    this.listeners.forEach((cb) => cb(s))
  }

  setQueue(tracks: Track[], startIndex = 0) {
    this.queue = tracks.slice()
    this.index = tracks.length ? Math.min(Math.max(startIndex, 0), tracks.length - 1) : -1
    this.rebuildShuffle()
    this.emit()
  }

  playTrackList(tracks: Track[], startIndex: number) {
    this.setQueue(tracks, startIndex)
    this.loadCurrent(true)
  }

  enqueue(track: Track) {
    this.queue = [...this.queue, track]
    if (this.index === -1) this.index = 0
    this.rebuildShuffle()
    this.emit()
  }

  removeAt(i: number) {
    if (i < 0 || i >= this.queue.length) return
    const wasCurrent = i === this.index
    this.queue = this.queue.filter((_, idx) => idx !== i)
    if (i < this.index) this.index--
    if (this.index >= this.queue.length) this.index = this.queue.length - 1
    this.rebuildShuffle()
    if (wasCurrent) this.loadCurrent(this.playing)
    this.emit()
  }

  moveItem(from: number, to: number) {
    if (from < 0 || from >= this.queue.length || to < 0 || to >= this.queue.length) return
    const currentId = this.index >= 0 ? this.queue[this.index]?.id : null
    const q = this.queue.slice()
    const [item] = q.splice(from, 1)
    q.splice(to, 0, item)
    this.queue = q
    if (currentId) {
      this.index = q.findIndex((t) => t.id === currentId)
    }
    this.rebuildShuffle()
    this.emit()
  }

  private loadCurrent(autoplay: boolean) {
    const t = this.getState().current
    if (!t) {
      this.playing = false
      this.emit()
      return
    }
    this.active.src = this.resolveSrc(t)
    this.active.load()
    this.currentTimeMs = 0
    if (autoplay) {
      void this.active.play()
      this.playing = true
    }
    this.preloadNext()
    this.emit()
  }

  private preloadNext() {
    const ni = this.peekNextIndex()
    if (ni < 0 || ni >= this.queue.length) return
    this.preload.src = this.resolveSrc(this.queue[ni])
    this.preload.load()
  }

  play() {
    if (this.index < 0 && this.queue.length) this.index = 0
    if (this.getState().current) {
      if (!this.active.src) this.loadCurrent(true)
      else {
        void this.active.play()
        this.playing = true
      }
    }
    this.emit()
  }

  pause() {
    this.active.pause()
    this.playing = false
    this.emit()
  }

  toggle() {
    if (this.playing) this.pause()
    else this.play()
  }

  private rebuildShuffle() {
    if (!this.shuffle) {
      this.shuffleOrder = []
      this.shufflePos = -1
      return
    }
    const idxs = this.queue.map((_, i) => i)
    // Fisher-Yates shuffle
    for (let i = idxs.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[idxs[i], idxs[j]] = [idxs[j], idxs[i]]
    }
    // ensure current track is first in the shuffle cycle
    if (this.index >= 0) {
      const at = idxs.indexOf(this.index)
      if (at > 0) [idxs[0], idxs[at]] = [idxs[at], idxs[0]]
    }
    this.shuffleOrder = idxs
    this.shufflePos = 0
  }

  private peekNextIndex(): number {
    if (this.queue.length === 0) return -1
    if (this.shuffle) {
      const np = this.shufflePos + 1
      if (np < this.shuffleOrder.length) return this.shuffleOrder[np]
      if (this.repeat === 'all') return this.shuffleOrder[0]
      return -1
    }
    const ni = this.index + 1
    if (ni < this.queue.length) return ni
    if (this.repeat === 'all') return 0
    return -1
  }

  private advance(dir: 1 | -1, fromEnded = false) {
    if (this.queue.length === 0) return
    if (this.shuffle) {
      let np = this.shufflePos + dir
      if (np >= this.shuffleOrder.length) {
        if (this.repeat === 'all') np = 0
        else {
          if (fromEnded) { this.playing = false; this.emit() }
          return
        }
      }
      if (np < 0) np = 0
      this.shufflePos = np
      this.index = this.shuffleOrder[np]
      this.loadCurrent(this.playing || fromEnded)
      return
    }
    let ni = this.index + dir
    if (ni >= this.queue.length) {
      if (this.repeat === 'all') ni = 0
      else {
        if (fromEnded) { this.playing = false; this.emit() }
        return
      }
    }
    if (ni < 0) ni = 0
    this.index = ni
    this.loadCurrent(this.playing || fromEnded)
  }

  playAt(index: number) {
    if (this.queue.length === 0 || index < 0 || index >= this.queue.length) return
    this.index = index
    if (this.shuffle) {
      // Align shufflePos so next/prev stay coherent from this index.
      const pos = this.shuffleOrder.indexOf(index)
      this.shufflePos = pos >= 0 ? pos : 0
    }
    this.loadCurrent(true)
  }

  next() {
    this.advance(1)
  }

  prev() {
    // restart current if >3s in, else go back
    if (this.currentTimeMs > 3000) {
      this.seekMs(0)
      return
    }
    this.advance(-1)
  }

  seekMs(ms: number) {
    const clamped = Math.max(0, this.durationMs > 0 ? Math.min(ms, this.durationMs) : ms)
    this.active.currentTime = clamped / 1000
    this.currentTimeMs = clamped
    this.emit()
  }

  setVolume(v: number) {
    this.volume = Math.min(1, Math.max(0, v))
    this.active.volume = this.volume
    this.preload.volume = this.volume
    this.emit()
  }

  toggleShuffle() {
    this.shuffle = !this.shuffle
    this.rebuildShuffle()
    this.emit()
  }

  cycleRepeat() {
    this.repeat = this.repeat === 'off' ? 'all' : this.repeat === 'all' ? 'one' : 'off'
    this.emit()
  }
}
