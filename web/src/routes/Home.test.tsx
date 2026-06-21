import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import Home from './Home'
import { useDownloads } from '../lib/downloadStore'
import type { Album, DownloadJob, Playlist } from '../lib/types'

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

function makeAlbum(overrides: Partial<Album> = {}): Album {
  return {
    id: 'al1',
    name: 'Test Album',
    artistId: 'ar1',
    artist: 'Test Artist',
    coverArtId: '',
    year: 2021,
    songCount: 10,
    durationMs: 180000,
    tracks: [],
    ...overrides,
  }
}

function makeJob(overrides: Partial<DownloadJob> = {}): DownloadJob {
  return {
    id: 'j1',
    dedupKey: 'dk1',
    status: 'completed',
    progress: 100,
    downloaderName: 'spotdl',
    priority: 0,
    attempts: 1,
    source: 'spotify',
    externalId: 'sp1',
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: Date.now() / 1000 - 5,
    finishedAt: Date.now() / 1000,
    title: 'Downloaded Track',
    artist: 'DL Artist',
    album: 'DL Album',
    ...overrides,
  }
}

// ------------------------------------------------------------------
// Mocks
// ------------------------------------------------------------------

vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    useAlbums: vi.fn(),
    usePlaylists: vi.fn(),
    coverUrl: vi.fn((id: string) => `/api/v1/cover/${id}`),
  }
})

vi.mock('../lib/playerStore', () => ({
  usePlayer: vi.fn((selector: (s: { current: null; playTrackList: ReturnType<typeof vi.fn> }) => unknown) => {
    const state = { current: null, playTrackList: vi.fn() }
    return typeof selector === 'function' ? selector(state) : state
  }),
  engine: { subscribe: vi.fn(() => () => {}), getState: vi.fn(() => ({})) },
}))

vi.mock('../lib/api', () => ({
  api: {
    get: vi.fn(async () => ({ id: 'al1', tracks: [] })),
  },
}))

// ------------------------------------------------------------------
// Suites
// ------------------------------------------------------------------

describe('Home feed', () => {
  beforeEach(() => {
    // Reset download store to empty state
    useDownloads.setState({ jobs: {} })
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders chip filter row with All, Music, Downloads when content exists', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [makeAlbum()], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Home />))

    expect(screen.getByRole('button', { name: /^all$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^music$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^downloads$/i })).toBeInTheDocument()
  })

  it('shows the first-run welcome state (not a blank void) when there is no content', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Home />))

    expect(screen.getByRole('heading', { name: /welcome to reverb/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /search music/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /connect a library/i })).toBeInTheDocument()
    // The orphan chip row is hidden in the empty state.
    expect(screen.queryByRole('button', { name: /^all$/i })).not.toBeInTheDocument()
  })

  it('renders shortcut grid and "Jump back in" carousel when data is present', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')

    const albums = [
      makeAlbum({ id: 'al1', name: 'OK Computer', artist: 'Radiohead' }),
      makeAlbum({ id: 'al2', name: 'Currents', artist: 'Tame Impala' }),
    ]

    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Home />))

    // Shortcut grid — at least one album name visible
    expect(screen.getAllByText('OK Computer').length).toBeGreaterThan(0)

    // "Jump back in" carousel heading
    expect(screen.getByRole('heading', { name: /jump back in/i })).toBeInTheDocument()
  })

  it('hides "Recently downloaded" section when there are no completed downloads', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')

    const albums = [makeAlbum({ id: 'al1', name: 'Album One', artist: 'Artist One' })]
    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    // No completed jobs in the store
    useDownloads.setState({ jobs: {} })

    render(wrap(<Home />))

    expect(screen.queryByText(/recently downloaded/i)).not.toBeInTheDocument()
  })

  it('shows "Recently downloaded" carousel when completed download jobs exist', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')

    const albums = [makeAlbum({ id: 'al1', name: 'Some Album', artist: 'Some Artist' })]
    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    const job = makeJob({ id: 'j1', status: 'completed', title: 'My DL Track', artist: 'DL Person', album: 'DL Collection' })
    useDownloads.setState({ jobs: { j1: job } })

    render(wrap(<Home />))

    expect(screen.getByRole('heading', { name: /recently downloaded/i })).toBeInTheDocument()
  })

  it('shows skeleton tiles while data is loading', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')

    vi.mocked(useAlbums).mockReturnValue({ data: undefined, isLoading: true, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: undefined, isLoading: true, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Home />))

    // Skeleton elements should be present
    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('hides "Just added to your library" hero when newest albums list is empty', async () => {
    const { useAlbums, usePlaylists } = await import('../lib/libraryApi')

    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Home />))

    expect(screen.queryByText(/just added to your library/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/jump back in/i)).not.toBeInTheDocument()
  })
})
