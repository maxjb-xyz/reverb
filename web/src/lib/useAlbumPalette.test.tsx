import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAlbumPalette } from './useAlbumPalette'
import { __setComputeFnForTests, __resetForTests } from './paletteService'
import type { RGB } from './palette'

// Mock useSettings so the gate is controllable.
vi.mock('./settingsApi', () => ({
  useSettings: vi.fn(),
}))
import { useSettings } from './settingsApi'

function setSettings(dynamicBackground: boolean | undefined) {
  vi.mocked(useSettings).mockReturnValue({
    data: dynamicBackground === undefined ? undefined : { accentColor: '#F0354B', dynamicBackground },
  } as ReturnType<typeof useSettings>)
}

describe('useAlbumPalette', () => {
  beforeEach(() => {
    __resetForTests()
    __setComputeFnForTests(async () => [200, 30, 40] as RGB)
  })

  it('returns null while settings are still loading', () => {
    setSettings(undefined)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null when dynamic_background is off', () => {
    setSettings(false)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null for an empty cover URL', () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette(''))
    expect(result.current).toBeNull()
  })

  it('resolves the palette with contrast text when on', async () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    await waitFor(() => expect(result.current).not.toBeNull())
    expect(result.current?.rgb).toEqual([200, 30, 40])
    // luminance of (200,30,40) < 0.5 → light text
    expect(result.current?.text).toBe('#FFFFFF')
    expect(typeof result.current?.scrim).toBe('boolean')
  })
})
