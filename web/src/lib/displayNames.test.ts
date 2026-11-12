import { describe, it, expect } from 'vitest'
import { adapterDisplayName, capabilityLabel } from './displayNames'

describe('adapterDisplayName', () => {
  it('uses the product spelling for known adapters', () => {
    expect(adapterDisplayName('deezer')).toBe('Deezer')
    expect(adapterDisplayName('spotdl')).toBe('spotDL')
    expect(adapterDisplayName('Lidarr')).toBe('Lidarr')
  })
  it('capitalises unknown keys', () => {
    expect(adapterDisplayName('tidal')).toBe('Tidal')
    expect(adapterDisplayName('')).toBe('')
  })
})

describe('capabilityLabel', () => {
  it('maps capability keys to the labels admins see', () => {
    expect(capabilityLabel('can_manage_users')).toBe('Manage users & roles')
    expect(capabilityLabel('auto_approve')).toBe('Auto-approve music')
  })
  it('never shows a raw key for an unknown capability', () => {
    expect(capabilityLabel('can_do_new_thing')).toBe('Do new thing')
  })
})
