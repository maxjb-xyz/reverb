import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'

const get = vi.fn()
const put = vi.fn()
const post = vi.fn()
vi.mock('../../lib/api', () => ({
  api: {
    get: (p: string) => get(p),
    put: (p: string, b?: unknown) => put(p, b),
    post: (p: string, b?: unknown) => post(p, b),
    del: vi.fn(),
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
vi.mock('../download/DownloadAction', () => ({
  DownloadAction: ({ result }: { result: { title: string } }) => <span>get {result.title}</span>,
}))

import { MixesRow } from './MixesRow'
import Discover from '../../routes/Discover'
import { DiscoverSettings } from '../account/DiscoverSettings'
import { discoverTracks, hasDownloading, refreshLabel, type DiscoverWeek } from '../../lib/discoverApi'

const track = (id: string) => ({ id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Bonobo', coverArtId: 'c', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg' })
const ext = (id: string) => ({ source: 'deezer', externalId: id, title: `New ${id}`, artist: 'Emancipator', album: 'Soon It Will Be Cold Enough', durationMs: 200000, type: 'track' as const })
const prefs = { enabled: true, size: 30, newShare: 70, keepRule: 'playlist' as const }
const week = (over: Partial<DiscoverWeek> = {}): DiscoverWeek => ({
  week: '2027-W02',
  startsAt: 1799625600,
  endsAt: 1800230400,
  prefs,
  downloads: true,
  items: [
    { id: 'i1', state: 'downloading', owned: true, track: track('a'), jobId: 'j1', jobStatus: 'completed', reason: { kind: 'discovery', artist: 'Emancipator', seedArtist: 'Bonobo' } },
    { id: 'i2', state: 'downloading', owned: false, external: ext('x'), jobId: 'j2', jobStatus: 'running', progress: 40, reason: { kind: 'discovery', artist: 'Emancipator', seedArtist: 'Bonobo' } },
    { id: 'i3', state: 'suggested', owned: true, track: track('b'), reason: { kind: 'library' } },
  ],
  ...over,
})

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  get.mockImplementation((p: string) => (p === '/mixes' ? Promise.resolve({ mixes: [] }) : Promise.reject(new Error(p))))
  put.mockImplementation((_p: string, b: unknown) => Promise.resolve(b))
})

describe('discoverApi helpers', () => {
  it('knows what is playable and what is still downloading', () => {
    expect(discoverTracks(week())).toEqual([track('a'), track('b')])
    expect(hasDownloading(week())).toBe(true)
    const done = week({ items: week().items.filter((i) => i.id !== 'i2') })
    expect(hasDownloading(done)).toBe(false)
    expect(refreshLabel(0)).toBe('New songs every Monday')
    expect(refreshLabel(1800230400)).toMatch(/^New songs \w+/)
  })
})

describe('Discover Weekly tile', () => {
  it('leads the mixes row and opens the page', async () => {
    wrap(<MixesRow />)
    fireEvent.click(await screen.findByRole('button', { name: 'Discover Weekly' }))
    expect(navigate).toHaveBeenCalledWith('/discover')
  })
})

describe('Discover page', () => {
  it('lists owned and incoming songs and plays the owned ones as Discover', async () => {
    get.mockImplementation((p: string) => (p === '/discover' ? Promise.resolve(week()) : Promise.reject(new Error(p))))
    wrap(<Discover />)
    expect(await screen.findByRole('heading', { name: 'Discover Weekly' })).toBeInTheDocument()
    expect(screen.getByTestId('discover-status')).toHaveTextContent('2 of 3 songs ready · fetching 1')
    const rows = within(screen.getByTestId('discover-tracks'))
    expect(rows.getByText('Song a')).toBeInTheDocument()
    expect(rows.getByText('get New x')).toBeInTheDocument()
    expect(screen.getByTestId('discover-keep-note')).toHaveTextContent('add to a playlist')
    fireEvent.click(screen.getByRole('button', { name: 'Play Discover Weekly' }))
    expect(player.playTrackList).toHaveBeenCalledWith([track('a'), track('b')], 0, { kind: 'discover', id: '2027-W02' })
  })

  it('numbers the songs from 1', async () => {
    get.mockImplementation((p: string) => (p === '/discover' ? Promise.resolve(week()) : Promise.reject(new Error(p))))
    wrap(<Discover />)
    const list = await screen.findByTestId('discover-tracks')
    expect(list.textContent).toMatch(/^1\D/)
  })

  it('says so when every pick is already in the library', async () => {
    get.mockImplementation((p: string) =>
      p === '/discover' ? Promise.resolve(week({ items: [week().items[2]] })) : Promise.reject(new Error(p)),
    )
    wrap(<Discover />)
    expect(await screen.findByTestId('discover-blurb')).toHaveTextContent('from your library for now')
  })

  it('swaps in new songs on demand', async () => {
    get.mockImplementation((p: string) => (p === '/discover' ? Promise.resolve(week()) : Promise.reject(new Error(p))))
    post.mockImplementation((p: string) =>
      p === '/discover/regenerate' ? Promise.resolve(week({ items: [{ ...week().items[2], id: 'i9', track: track('z') }] })) : Promise.reject(new Error(p)),
    )
    wrap(<Discover />)
    fireEvent.click(await screen.findByRole('button', { name: 'New songs' }))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/discover/regenerate', undefined))
    expect(await screen.findByText('Song z')).toBeInTheDocument()
  })

  it('says when Discover Weekly is off', async () => {
    get.mockImplementation((p: string) => (p === '/discover' ? Promise.resolve(week({ prefs: { ...prefs, enabled: false }, items: [] })) : Promise.reject(new Error(p))))
    wrap(<Discover />)
    expect(await screen.findByText('Discover Weekly is off')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open settings' })).toHaveAttribute('href', '/settings/recommendations')
  })

  it('has no keep note when nothing is downloaded for this user', async () => {
    get.mockImplementation((p: string) => (p === '/discover' ? Promise.resolve(week({ downloads: false })) : Promise.reject(new Error(p))))
    wrap(<Discover />)
    await screen.findByRole('heading', { name: 'Discover Weekly' })
    expect(screen.queryByTestId('discover-keep-note')).toBeNull()
    expect(screen.getByText(/downloaded or requested one by one/)).toBeInTheDocument()
  })
})

describe('Discover settings', () => {
  it('saves the keep rule; playlist-only is the default', async () => {
    get.mockImplementation((p: string) => (p === '/discover/prefs' ? Promise.resolve(prefs) : Promise.reject(new Error(p))))
    wrap(<DiscoverSettings isManager={false} />)
    const keep = (await screen.findByLabelText('Keep downloaded songs')) as HTMLSelectElement
    expect(keep.value).toBe('playlist')
    expect(screen.getByLabelText('Discover mix')).toHaveValue('70')
    fireEvent.change(keep, { target: { value: 'played' } })
    await waitFor(() => expect(put).toHaveBeenCalledWith('/discover/prefs', { ...prefs, keepRule: 'played' }))
    expect(screen.queryByText(/Download Discover songs ahead/)).toBeNull()
  })

  it('lets managers switch pre-downloads and set the weekly limit', async () => {
    get.mockImplementation((p: string) =>
      p === '/discover/prefs' ? Promise.resolve(prefs) : p === '/discover/settings' ? Promise.resolve({ downloads: true, weeklyCap: 30 }) : Promise.reject(new Error(p)),
    )
    wrap(<DiscoverSettings isManager />)
    const cap = (await screen.findByLabelText('Weekly download limit')) as HTMLSelectElement
    expect(cap.value).toBe('30')
    fireEvent.change(cap, { target: { value: '50' } })
    await waitFor(() => expect(put).toHaveBeenCalledWith('/discover/settings', { downloads: true, weeklyCap: 50 }))
  })
})
