import { describe, it, expect } from 'vitest'
import { contextFromPath, getPlayContext, setPlayContext } from './playContext'

describe('playContext', () => {
  it('infers the context from the route', () => {
    expect(contextFromPath('/')).toEqual({ kind: 'home' })
    expect(contextFromPath('/album/library/al-1')).toEqual({ kind: 'album', id: 'al-1' })
    expect(contextFromPath('/playlist/pl%201')).toEqual({ kind: 'playlist', id: 'pl 1' })
    expect(contextFromPath('/search')).toEqual({ kind: 'search' })
    expect(contextFromPath('/settings/profile')).toEqual({ kind: 'other' })
  })

  it('stores an explicit context', () => {
    setPlayContext({ kind: 'radio', id: 'st-1' })
    expect(getPlayContext()).toEqual({ kind: 'radio', id: 'st-1' })
    setPlayContext({ kind: '' })
    expect(getPlayContext()).toEqual({ kind: 'other' })
  })
})
