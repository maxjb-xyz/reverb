import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { hexToRgbChannels, applyAccent, getSettings } from './settingsApi'

describe('hexToRgbChannels', () => {
  it('converts #F0354B to space-separated RGB channels', () => {
    expect(hexToRgbChannels('#F0354B')).toBe('240 53 75')
  })
  it('handles a hex without the leading #', () => {
    expect(hexToRgbChannels('00FF88')).toBe('0 255 136')
  })
  it('returns the default red channels for an invalid hex', () => {
    expect(hexToRgbChannels('nope')).toBe('240 53 75')
  })
})

describe('applyAccent', () => {
  it('sets the --color-accent CSS var on <html>', () => {
    applyAccent('#00FF88')
    expect(document.documentElement.style.getPropertyValue('--color-accent')).toBe('0 255 136')
  })
})

describe('getSettings', () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    fetchMock.mockReset()
  })
  afterEach(() => vi.unstubAllGlobals())
  it('GETs /settings', async () => {
    fetchMock.mockReturnValue(
      Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify({ accentColor: '#F0354B', dynamicBackground: true })) } as Response),
    )
    const s = await getSettings()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/settings', expect.objectContaining({ method: 'GET' }))
    expect(s.accentColor).toBe('#F0354B')
  })
})
