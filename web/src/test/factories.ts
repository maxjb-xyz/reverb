import type { Track, Album } from '../lib/types'

export function makeTrack(overrides?: Partial<Track>): Track {
  return {
    id: 't1',
    title: 'Test Track',
    albumId: 'al1',
    album: 'Test Album',
    artistId: 'ar1',
    artist: 'Test Artist',
    coverArtId: '',
    trackNumber: 1,
    discNumber: 1,
    durationMs: 180000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
    ...overrides,
  }
}

export function makeAlbum(overrides?: Partial<Album>): Album {
  return {
    id: 'al1',
    name: 'Test Album',
    artistId: 'ar1',
    artist: 'Test Artist',
    coverArtId: '',
    year: 2021,
    songCount: 1,
    durationMs: 180000,
    ...overrides,
  }
}
