import { describe, it, expect } from 'vitest'
import { contextFromPath, getPlayContext, setPlayContext, setTrackContexts } from './playContext'

describe('playContext', () => {
  it('infers the context from the route', () => {
    expect(contextFromPath('/')).toEqual({ kind: 'home' })
    expect(contextFromPath('/album/library/al-1')).toEqual({ kind: 'album', id: 'al-1' })
    expect(contextFromPath('/playlist/pl%201')).toEqual({ kind: 'playlist', id: 'pl 1' })
    expect(contextFromPath('/mix/m1')).toEqual({ kind: 'mix', id: 'm1' })
    expect(contextFromPath('/search')).toEqual({ kind: 'search' })
    expect(contextFromPath('/settings/profile')).toEqual({ kind: 'other' })
  })

  it('stores an explicit context', () => {
    setPlayContext({ kind: 'radio', id: 'st-1' })
    expect(getPlayContext()).toEqual({ kind: 'radio', id: 'st-1' })
    setPlayContext({ kind: '' })
    expect(getPlayContext()).toEqual({ kind: 'other' })
  })

  it('lets a track carry a finer context while the queue is of the same kind', () => {
    setTrackContexts([['t1', { kind: 'dj', id: 'lane:on-repeat:taste' }]], true)
    setPlayContext({ kind: 'dj', id: 'dj' })
    expect(getPlayContext('t1')).toEqual({ kind: 'dj', id: 'lane:on-repeat:taste' })
    expect(getPlayContext('t2')).toEqual({ kind: 'dj', id: 'dj' })
    expect(getPlayContext()).toEqual({ kind: 'dj', id: 'dj' })
    // The same song played later from an album is the album's.
    setPlayContext({ kind: 'album', id: 'al' })
    expect(getPlayContext('t1')).toEqual({ kind: 'album', id: 'al' })
    setTrackContexts([], true)
    setPlayContext({ kind: 'dj', id: 'dj' })
    expect(getPlayContext('t1')).toEqual({ kind: 'dj', id: 'dj' })
  })
})
