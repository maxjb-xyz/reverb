import { describe, it, expect } from 'vitest'
import { pageKey } from './pageKey'

describe('pageKey', () => {
  it('is the pathname for ordinary pages', () => {
    expect(pageKey('/album/library/a1')).toBe('/album/library/a1')
    expect(pageKey('/')).toBe('/')
  })
  it('treats every Settings section as one page', () => {
    expect(pageKey('/settings')).toBe('/settings')
    expect(pageKey('/settings/providers')).toBe('/settings')
    expect(pageKey('/admin')).toBe('/settings')
  })
})
