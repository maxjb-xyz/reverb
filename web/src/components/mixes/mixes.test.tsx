import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const get = vi.fn()
const post = vi.fn()
const put = vi.fn()
const del = vi.fn()
vi.mock('../../lib/api', () => ({
  api: {
    get: (p: string) => get(p),
    post: (p: string, b?: unknown) => post(p, b),
    put: (p: string, b?: unknown) => put(p, b),
    del: (p: string) => del(p),
  },
}))
const navigate = vi.fn()
vi.mock('react-router-dom', async (orig) => ({ ...(await orig<typeof import('react-router-dom')>()), useNavigate: () => navigate }))
const player = { playTrackList: vi.fn(), toggleShuffle: vi.fn(), current: null, playing: false, shuffle: false }
vi.mock('../../lib/playerStore', () => ({
  usePlayer: (sel: (s: typeof player) => unknown) => sel(player),
}))
const push = vi.fn()
vi.mock('../../lib/toastStore', () => ({
  useToastStore: (sel: (s: { push: typeof push }) => unknown) => sel({ push }),
}))
vi.mock('../../lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))

import { MixesRow } from './MixesRow'
import { MixEditor } from './MixEditor'
import MixPage from '../../routes/Mix'
import type { Mix } from '../../lib/mixesApi'

const track = (id: string) => ({ id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Bonobo', coverArtId: 'c', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg' })
const mix = (over: Partial<Mix> = {}): Mix => ({
  id: 'm1', kind: 'daily', name: 'Daily Mix 1', description: 'Bonobo, Tycho and more',
  recipe: { seeds: [{ kind: 'artist', id: 'bonobo', artist: 'Bonobo' }], size: 30, refresh: 'daily' },
  items: [
    { owned: true, track: track('a'), score: 1, reason: { kind: 'seed_artist', artist: 'Bonobo' } },
    { owned: true, track: track('b'), score: 1, reason: { kind: 'similar_artist', artist: 'Tycho', seedArtist: 'Bonobo' } },
  ],
  coverArtId: 'c', generatedAt: Math.floor(Date.now() / 1000), createdAt: 1, ...over,
})

function wrap(ui: ReactNode, path = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/mix/:id" element={ui} />
          <Route path="*" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  get.mockImplementation((p: string) => {
    if (p === '/library/artists') return Promise.resolve([{ id: 'ar1', name: 'Bonobo', coverArtId: '', albumCount: 2 }])
    if (p === '/engine/genres') return Promise.resolve({ genres: [{ key: 'jazz', label: 'Jazz', tracks: 40 }] })
    return Promise.reject(new Error(p))
  })
})

describe('MixesRow', () => {
  it('shows mixes, plays one as a mix and offers a new one', async () => {
    const base = get.getMockImplementation()!
    get.mockImplementation((p: string) => (p === '/mixes' ? Promise.resolve({ mixes: [mix()] }) : base(p)))
    wrap(<MixesRow />)
    expect(await screen.findByText('Your mixes')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Daily Mix 1'))
    expect(navigate).toHaveBeenCalledWith('/mix/m1')
    fireEvent.click(screen.getByRole('button', { name: /play daily mix 1/i }))
    expect(player.playTrackList).toHaveBeenCalledWith([track('a'), track('b')], 0, { kind: 'mix', id: 'm1' })
    fireEvent.click(screen.getByText('New mix'))
    expect(await screen.findByRole('dialog', { name: 'New mix' })).toBeInTheDocument()
  })

  it('rebuilds the daily mixes on demand', async () => {
    const base = get.getMockImplementation()!
    get.mockImplementation((p: string) => (p === '/mixes' ? Promise.resolve({ mixes: [mix()] }) : base(p)))
    post.mockImplementation((p: string) => (p === '/mixes/daily/rebuild' ? Promise.resolve({ mixes: [mix({ name: 'Daily Mix 2' })] }) : Promise.reject(new Error(p))))
    wrap(<MixesRow />)
    fireEvent.click(await screen.findByRole('button', { name: 'Refresh daily mixes' }))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/mixes/daily/rebuild', undefined))
    expect(await screen.findByText('Daily Mix 2')).toBeInTheDocument()
  })
})

describe('MixEditor', () => {
  it('builds a recipe from artists, genres, style and length', async () => {
    post.mockResolvedValue(mix({ id: 'm9', kind: 'custom', name: 'Late night' }))
    const onSaved = vi.fn()
    wrap(<MixEditor onClose={vi.fn()} onSaved={onSaved} />)
    await waitFor(() => expect(get).toHaveBeenCalledWith('/engine/genres'))
    fireEvent.change(screen.getByLabelText('Mix name'), { target: { value: 'Late night' } })
    fireEvent.change(screen.getByLabelText('Artist to add'), { target: { value: 'bonobo' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.change(screen.getByLabelText('Seed type'), { target: { value: 'genre' } })
    await screen.findByLabelText('Genre to add')
    fireEvent.change(screen.getByLabelText('Genre to add'), { target: { value: 'Jazz' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByRole('button', { name: 'Remove Jazz' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Style'), { target: { value: 'deep-cuts' } })
    fireEvent.change(screen.getByLabelText('Length'), { target: { value: '50' } })
    fireEvent.change(screen.getByLabelText('Refresh'), { target: { value: 'weekly' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mix' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/mixes', {
        name: 'Late night',
        recipe: { seeds: [{ kind: 'artist', artist: 'Bonobo' }, { kind: 'genre', id: 'jazz' }], lane: 'deep-cuts', size: 50, refresh: 'weekly' },
      }),
    )
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
  })

  it('rejects an unknown genre and a missing name, and needs years for a throwback', async () => {
    wrap(<MixEditor onClose={vi.fn()} />)
    await waitFor(() => expect(get).toHaveBeenCalledWith('/engine/genres'))
    fireEvent.change(screen.getByLabelText('Seed type'), { target: { value: 'genre' } })
    fireEvent.change(await screen.findByLabelText('Genre to add'), { target: { value: 'Polka' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByRole('alert')).toHaveTextContent('No "Polka" genre')
    fireEvent.click(screen.getByRole('button', { name: 'Create mix' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Give the mix a name')
    fireEvent.change(screen.getByLabelText('Mix name'), { target: { value: '90s' } })
    fireEvent.change(screen.getByLabelText('Style'), { target: { value: 'throwback' } })
    fireEvent.change(screen.getByLabelText('From year'), { target: { value: '1999' } })
    fireEvent.change(screen.getByLabelText('To year'), { target: { value: '1990' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create mix' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Pick a period')
    expect(post).not.toHaveBeenCalled()
  })
})

describe('Mix page', () => {
  it('lists the songs with why they were picked and plays them as the mix', async () => {
    const base = get.getMockImplementation()!
    get.mockImplementation((p: string) => (p === '/mixes/m1' ? Promise.resolve(mix()) : base(p)))
    wrap(<MixPage />, '/mix/m1')
    expect(await screen.findByRole('heading', { name: 'Daily Mix 1' })).toBeInTheDocument()
    expect(screen.getByText('Fans of Bonobo play Tycho')).toBeInTheDocument()
    expect(screen.getByTestId('mix-tracks').textContent).toMatch(/^1\D/)
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument() // daily mixes are Reverb's
    fireEvent.click(screen.getAllByRole('button', { name: 'Play Daily Mix 1' })[0])
    expect(player.playTrackList).toHaveBeenCalledWith([track('a'), track('b')], 0, { kind: 'mix', id: 'm1' })
  })

  it('refreshes, edits and deletes a custom mix', async () => {
    const base = get.getMockImplementation()!
    const custom = mix({ kind: 'custom', name: 'Late night' })
    get.mockImplementation((p: string) => (p === '/mixes/m1' ? Promise.resolve(custom) : base(p)))
    post.mockResolvedValue(custom)
    del.mockResolvedValue(null)
    wrap(<MixPage />, '/mix/m1')
    await screen.findByRole('heading', { name: 'Late night' })
    fireEvent.click(screen.getByRole('button', { name: 'New songs' }))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/mixes/m1/refresh', undefined))
    await waitFor(() => expect(push).toHaveBeenCalledWith('Fresh songs in your mix', 'success'))
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(await screen.findByRole('dialog', { name: 'Edit mix' })).toBeInTheDocument()
    expect(screen.getByLabelText('Mix name')).toHaveValue('Late night')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete mix' }))
    await waitFor(() => expect(del).toHaveBeenCalledWith('/mixes/m1'))
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/'))
  })
})
