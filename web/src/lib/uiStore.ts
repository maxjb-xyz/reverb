import { create } from 'zustand'

// RightPanel models the single right-side slot. M1 ships 'queue'. M3 adds
// 'downloads' (Download Tray) into the SAME slot — opening one closes the other.
export type RightPanel = 'queue' | 'downloads' | null

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
}))
