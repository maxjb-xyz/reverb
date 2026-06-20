import { create } from 'zustand'
import { AudioEngine, type PlayerState } from './audioEngine'
import type { Track } from './types'

// Single imperative engine instance, living OUTSIDE React.
export const engine = new AudioEngine()

interface PlayerActions {
  playTrackList(tracks: Track[], startIndex: number): void
  enqueue(t: Track): void
  removeAt(i: number): void
  moveItem(from: number, to: number): void
  play(): void
  pause(): void
  toggle(): void
  next(): void
  prev(): void
  seekMs(ms: number): void
  setVolume(v: number): void
  toggleShuffle(): void
  cycleRepeat(): void
}

export type PlayerStore = PlayerState & PlayerActions

export const usePlayer = create<PlayerStore>((set) => {
  // Mirror engine state into the store on every change.
  engine.subscribe((s) => set(s))
  return {
    ...engine.getState(),
    playTrackList: (tracks, startIndex) => engine.playTrackList(tracks, startIndex),
    enqueue: (t) => engine.enqueue(t),
    removeAt: (i) => engine.removeAt(i),
    moveItem: (from, to) => engine.moveItem(from, to),
    play: () => engine.play(),
    pause: () => engine.pause(),
    toggle: () => engine.toggle(),
    next: () => engine.next(),
    prev: () => engine.prev(),
    seekMs: (ms) => engine.seekMs(ms),
    setVolume: (v) => engine.setVolume(v),
    toggleShuffle: () => engine.toggleShuffle(),
    cycleRepeat: () => engine.cycleRepeat(),
  }
})
