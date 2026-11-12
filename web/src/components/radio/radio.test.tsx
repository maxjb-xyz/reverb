import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const get = vi.fn()
const post = vi.fn()
vi.mock('../../lib/api', () => ({
  api: { get: (p: string) => get(p), post: (p: string, b?: unknown) => post(p, b) },
}))
const navigate = vi.fn()
vi.mock('react-router-dom', async (orig) => ({ ...(await orig<typeof import('react-router-dom')>()), useNavigate: () => navigate }))
const playTrackList = vi.fn()
vi.mock('../../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))
const push = vi.fn()
vi.mock('../../lib/toastStore', () => ({
  useToastStore: (sel: (s: { push: typeof push }) => unknown) => sel({ push }),
}))

import { RadioButton } from './RadioButton'
import { StationsRow } from './StationsRow'
import { FansAlsoLike } from './FansAlsoLike'
import { _resetRadio } from '../../lib/radio'
import { reasonText } from '../../lib/engineApi'

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

const track = { id: 't1', title: 'Kerala', albumId: 'al', album: 'Migration', artistId: 'ar', artist: 'Bonobo', coverArtId: 'c', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg' }

beforeEach(() => {
  vi.clearAllMocks()
  _resetRadio()
})

describe('RadioButton', () => {
  it('starts the station, plays it as radio and says so', async () => {
    post.mockResolvedValue({ station: { id: 'artist:bonobo', kind: 'artist', label: 'Bonobo radio' }, items: [{ owned: true, track, score: 1, reason: { kind: 'seed_artist' } }] })
    wrap(<RadioButton seed={{ kind: 'artist', artist: 'Bonobo' }} label="Artist radio" />)
    fireEvent.click(screen.getByRole('button', { name: /Artist radio/ }))
    await waitFor(() => expect(playTrackList).toHaveBeenCalledWith([track], 0, { kind: 'radio', id: 'artist:bonobo' }))
    expect(post).toHaveBeenCalledWith('/engine/next', { seed: { kind: 'artist', artist: 'Bonobo' }, n: 20 })
    expect(push).toHaveBeenCalledWith('Playing Bonobo radio', 'success')
  })

  it('explains an empty station instead of playing nothing', async () => {
    post.mockResolvedValue({ station: { id: 'genre:jazz', kind: 'genre', label: 'Jazz radio' }, items: [] })
    wrap(<RadioButton seed={{ kind: 'genre', id: 'jazz' }} look="icon" label="Song radio" />)
    fireEvent.click(screen.getByRole('button', { name: 'Song radio' }))
    await waitFor(() => expect(push).toHaveBeenCalledWith(expect.stringMatching(/Not enough in your library/), 'error'))
    expect(playTrackList).not.toHaveBeenCalled()
  })

  it('reports a failed request', async () => {
    post.mockRejectedValue(new Error('500'))
    wrap(<RadioButton seed={{ kind: 'track', id: 't1' }} />)
    fireEvent.click(screen.getByRole('button', { name: /Radio/ }))
    await waitFor(() => expect(push).toHaveBeenCalledWith("Couldn't start the radio.", 'error'))
  })
})

describe('StationsRow', () => {
  it('lists suggested stations and starts one on click', async () => {
    get.mockResolvedValue({
      stations: [
        { seed: { kind: 'artist', id: 'bonobo', artist: 'Bonobo' }, label: 'Bonobo radio', sublabel: 'Artist radio', coverArtId: 'c1' },
        { seed: { kind: 'genre', id: 'electronic' }, label: 'Electronic radio', sublabel: 'Genre radio' },
      ],
    })
    post.mockResolvedValue({ station: { id: 'genre:electronic', kind: 'genre', label: 'Electronic radio' }, items: [{ owned: true, track, score: 1, reason: { kind: 'genre' } }] })
    wrap(<StationsRow />)
    expect(await screen.findByText('Radio for you')).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith('/engine/stations?n=10')
    fireEvent.click(screen.getByText('Electronic radio'))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/engine/next', { seed: { kind: 'genre', id: 'electronic' }, n: 20 }))
  })

  it('renders nothing without suggestions', async () => {
    get.mockResolvedValue({ stations: [] })
    const { container } = wrap(<StationsRow />)
    await waitFor(() => expect(get).toHaveBeenCalled())
    expect(container).toBeEmptyDOMElement()
  })
})

describe('FansAlsoLike', () => {
  it('links library artists to their page and the rest to their online page', async () => {
    get.mockResolvedValue({
      artist: 'Bonobo',
      similar: [
        { key: 'tycho', name: 'Tycho', source: 'deezer', externalId: 'd1', rank: 0, ownedTracks: 8, libraryArtistId: 'ar-tycho' },
        { key: 'emancipator', name: 'Emancipator', source: 'deezer', externalId: 'd2', rank: 1, ownedTracks: 0 },
        { key: 'nolink', name: 'No Link', source: 'deezer', rank: 2, ownedTracks: 0 },
      ],
    })
    wrap(<FansAlsoLike artist="Bonobo" />)
    expect(await screen.findByText('Fans also like')).toBeInTheDocument()
    expect(get).toHaveBeenCalledWith('/artists/similar?name=Bonobo')
    expect(screen.queryByText('No Link')).not.toBeInTheDocument()
    fireEvent.click(screen.getByText('Tycho'))
    expect(navigate).toHaveBeenCalledWith('/artist/library/ar-tycho')
    fireEvent.click(screen.getByText('Emancipator'))
    expect(navigate).toHaveBeenCalledWith('/artist/deezer/d2')
    expect(screen.getByText('Not in your library')).toBeInTheDocument()
  })
})

describe('reasonText', () => {
  it('turns reasons into short lines', () => {
    expect(reasonText({ kind: 'similar_artist', artist: 'Tycho', seedArtist: 'Bonobo' })).toBe('Fans of Bonobo play Tycho')
    expect(reasonText({ kind: 'on_repeat', playCount: 6 })).toBe("You've played this 6×")
    expect(reasonText({ kind: 'genre', genre: 'Jazz' })).toBe('More Jazz')
    expect(reasonText({ kind: 'library' })).toBe('From your library')
  })
})
