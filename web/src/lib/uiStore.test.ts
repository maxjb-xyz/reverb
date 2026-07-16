import { beforeEach, describe, expect, it } from 'vitest'
import { useUI } from './uiStore'
import type { RightPanel } from './uiStore'

// Zustand stores are module singletons shared across tests, so reset the
// single right-panel slot to a known state before each test.
beforeEach(() => {
  useUI.setState({ rightPanel: null as RightPanel })
})

describe('uiStore right-panel slot', () => {
  it('starts with no panel open', () => {
    expect(useUI.getState().rightPanel).toBe(null)
  })

  it('openPanel("queue") opens the queue', () => {
    useUI.getState().openPanel('queue')
    expect(useUI.getState().rightPanel).toBe('queue')
  })

  it('openPanel replaces an already-open panel (mutual exclusion)', () => {
    useUI.getState().openPanel('queue')
    expect(useUI.getState().rightPanel).toBe('queue')
    useUI.getState().openPanel('downloads')
    expect(useUI.getState().rightPanel).toBe('downloads')
  })

  it('togglePanel closes the open panel and opens from null', () => {
    useUI.getState().openPanel('downloads')
    useUI.getState().togglePanel('downloads')
    expect(useUI.getState().rightPanel).toBe(null)
    useUI.getState().togglePanel('queue')
    expect(useUI.getState().rightPanel).toBe('queue')
  })

  it('closePanel clears the slot', () => {
    useUI.getState().openPanel('queue')
    useUI.getState().closePanel()
    expect(useUI.getState().rightPanel).toBe(null)
  })

  it('togglePanel("nowplaying") opens then closes', () => {
    useUI.getState().togglePanel('nowplaying')
    expect(useUI.getState().rightPanel).toBe('nowplaying')
    useUI.getState().togglePanel('nowplaying')
    expect(useUI.getState().rightPanel).toBe(null)
  })

  it('opening "downloads" replaces "nowplaying"', () => {
    useUI.getState().openPanel('nowplaying')
    expect(useUI.getState().rightPanel).toBe('nowplaying')
    useUI.getState().openPanel('downloads')
    expect(useUI.getState().rightPanel).toBe('downloads')
  })
})

describe('uiStore now-playing overlay', () => {
  beforeEach(() => {
    useUI.setState({ nowPlayingOpen: false })
  })

  it('starts closed', () => {
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('openNowPlaying opens it', () => {
    useUI.getState().openNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(true)
  })

  it('closeNowPlaying closes it', () => {
    useUI.getState().openNowPlaying()
    useUI.getState().closeNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('toggleNowPlaying flips it', () => {
    useUI.getState().toggleNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(true)
    useUI.getState().toggleNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })
})

describe('cinema', () => {
  beforeEach(() => useUI.setState({ cinemaOpen: false }))

  it('opens, closes and toggles the cinema view', () => {
    useUI.getState().openCinema()
    expect(useUI.getState().cinemaOpen).toBe(true)
    useUI.getState().closeCinema()
    expect(useUI.getState().cinemaOpen).toBe(false)
    useUI.getState().toggleCinema()
    expect(useUI.getState().cinemaOpen).toBe(true)
  })
})
