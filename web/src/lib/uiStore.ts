import { create } from 'zustand'

// RightPanel models the single right-side slot. M1 ships 'queue'. M3 adds
// 'downloads' (Download Tray). Phase 3 adds 'nowplaying' (desktop Now-Playing
// panel). All three share the same slot — opening one closes the other.
export type RightPanel = 'queue' | 'downloads' | 'nowplaying' | null

interface UIStore {
  rightPanel: RightPanel
  openPanel(p: Exclude<RightPanel, null>): void
  closePanel(): void
  togglePanel(p: Exclude<RightPanel, null>): void
  // nowPlayingOpen drives the MOBILE fullscreen now-playing overlay (M4b). It is the
  // single source of truth — the desktop player bar never reads it.
  nowPlayingOpen: boolean
  openNowPlaying(): void
  closeNowPlaying(): void
  toggleNowPlaying(): void
  // Cinema is the desktop fullscreen player. Mobile continues to use
  // nowPlayingOpen, so both views can share the player state safely.
  cinemaOpen: boolean
  openCinema(): void
  closeCinema(): void
  toggleCinema(): void
  // Lyrics is the desktop fullscreen lyrics view. It shares the "one fullscreen
  // player surface" slot with cinema — opening either closes the other.
  lyricsOpen: boolean
  openLyrics(): void
  closeLyrics(): void
  toggleLyrics(): void
}

export const useUI = create<UIStore>((set, get) => ({
  rightPanel: null,
  openPanel: (p) => set({ rightPanel: p }),
  closePanel: () => set({ rightPanel: null }),
  togglePanel: (p) => set({ rightPanel: get().rightPanel === p ? null : p }),
  nowPlayingOpen: false,
  openNowPlaying: () => set({ nowPlayingOpen: true }),
  closeNowPlaying: () => set({ nowPlayingOpen: false }),
  toggleNowPlaying: () => set({ nowPlayingOpen: !get().nowPlayingOpen }),
  cinemaOpen: false,
  openCinema: () => set({ cinemaOpen: true, lyricsOpen: false }),
  closeCinema: () => set({ cinemaOpen: false }),
  toggleCinema: () => set({ cinemaOpen: !get().cinemaOpen, lyricsOpen: false }),
  lyricsOpen: false,
  openLyrics: () => set({ lyricsOpen: true, cinemaOpen: false }),
  closeLyrics: () => set({ lyricsOpen: false }),
  toggleLyrics: () => set({ lyricsOpen: !get().lyricsOpen, cinemaOpen: false }),
}))
