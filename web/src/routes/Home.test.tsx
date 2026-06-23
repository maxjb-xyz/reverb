import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import Home from './Home'
import { useDownloads } from '../lib/downloadStore'
import type { Album, DownloadJob, SyncedPlaylist } from '../lib/types'

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
    coverUrl: vi.fn((id: string) => `/api/v1/cover/${id}`),
  }
})

vi.mock('../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylists: vi.fn(() => ({ data: [], isLoading: false, error: null })),
}))

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

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

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
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [makeAlbum()], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    expect(screen.getByRole('button', { name: /^all$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^music$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^downloads$/i })).toBeInTheDocument()
  })

  it('shows the first-run welcome state (not a blank void) when there is no content', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    expect(screen.getByRole('heading', { name: /welcome to reverb/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /search music/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /connect a library/i })).toBeInTheDocument()
    // The orphan chip row is hidden in the empty state.
    expect(screen.queryByRole('button', { name: /^all$/i })).not.toBeInTheDocument()
  })

  it('renders shortcut grid and "Jump back in" carousel when data is present', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const albums = [
      makeAlbum({ id: 'al1', name: 'OK Computer', artist: 'Radiohead' }),
      makeAlbum({ id: 'al2', name: 'Currents', artist: 'Tame Impala' }),
    ]

    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    // Shortcut grid — at least one album name visible
    expect(screen.getAllByText('OK Computer').length).toBeGreaterThan(0)

    // "Jump back in" carousel heading
    expect(screen.getByRole('heading', { name: /jump back in/i })).toBeInTheDocument()
  })

  it('hides "Recently downloaded" section when there are no completed downloads', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const albums = [makeAlbum({ id: 'al1', name: 'Album One', artist: 'Artist One' })]
    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    // No completed jobs in the store
    useDownloads.setState({ jobs: {} })

    render(wrap(<Home />))

    expect(screen.queryByText(/recently downloaded/i)).not.toBeInTheDocument()
  })

  it('shows "Recently downloaded" carousel when completed download jobs exist', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const albums = [makeAlbum({ id: 'al1', name: 'Some Album', artist: 'Some Artist' })]
    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    const job = makeJob({ id: 'j1', status: 'completed', title: 'My DL Track', artist: 'DL Person', album: 'DL Collection' })
    useDownloads.setState({ jobs: { j1: job } })

    render(wrap(<Home />))

    expect(screen.getByRole('heading', { name: /recently downloaded/i })).toBeInTheDocument()
  })

  it('completed job with coverArtId renders MediaCard with correct cover src', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const albums = [makeAlbum({ id: 'al1', name: 'Some Album', artist: 'Some Artist' })]
    vi.mocked(useAlbums).mockReturnValue({ data: albums, isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    const job = makeJob({ id: 'j2', status: 'completed', title: 'Cover Track', coverArtId: 'mf-abc123' })
    useDownloads.setState({ jobs: { j2: job } })

    render(wrap(<Home />))

    // The MediaCard should render an img with src derived from coverArtId 'mf-abc123'
    const img = screen.getByRole('img', { name: /Cover Track/i })
    expect(img).toHaveAttribute('src', '/api/v1/cover/mf-abc123')
  })

  it('shows skeleton tiles while data is loading', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    vi.mocked(useAlbums).mockReturnValue({ data: undefined, isLoading: true, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    // Skeleton elements should be present
    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('hides "Just added to your library" hero when newest albums list is empty', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    expect(screen.queryByText(/just added to your library/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/jump back in/i)).not.toBeInTheDocument()
  })

  it('clicking the hero album cover navigates to the album detail page', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const album = makeAlbum({ id: 'al99', name: 'Heros Cover Album', artist: 'Test Artist' })
    vi.mocked(useAlbums).mockReturnValue({ data: [album], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    // The cover is a <Link> (renders as <a>) with a distinct aria-label
    const coverLink = screen.getByRole('link', { name: 'Open album Heros Cover Album' })
    expect(coverLink).toHaveAttribute('href', '/album/library/al99')
  })

  it('clicking the hero album title navigates to the album detail page', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const album = makeAlbum({ id: 'al99', name: 'Heros Cover Album', artist: 'Test Artist' })
    vi.mocked(useAlbums).mockReturnValue({ data: [album], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    // The title is a <Link> inside an <h1>
    const titleLink = screen.getByRole('link', { name: 'Heros Cover Album' })
    expect(titleLink).toHaveAttribute('href', '/album/library/al99')
  })

  it('hero section has min-w-0 and overflow-hidden, and h1 has truncate, with a very long album title', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const longTitle = 'A'.repeat(200)
    const album = makeAlbum({ id: 'al-long', name: longTitle, artist: 'Test Artist' })
    vi.mocked(useAlbums).mockReturnValue({ data: [album], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    const heroSection = screen.getByRole('region', { name: /just added to your library/i })
    expect(heroSection.classList.contains('min-w-0')).toBe(true)
    expect(heroSection.classList.contains('overflow-hidden')).toBe(true)

    const h1 = heroSection.querySelector('h1')
    expect(h1).not.toBeNull()
    expect(h1!.classList.contains('truncate')).toBe(true)
  })

  it('clicking the hero Play button does NOT navigate — calls play instead', async () => {
    const { useAlbums } = await import('../lib/libraryApi')

    const album = makeAlbum({ id: 'al99', name: 'Heros Cover Album', artist: 'Test Artist' })
    vi.mocked(useAlbums).mockReturnValue({ data: [album], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Home />))

    mockNavigate.mockClear()

    // The hero section has its own Play button — locate it within the section landmark
    const heroSection = screen.getByRole('region', { name: /just added to your library/i })
    const playButton = heroSection.querySelector('button[aria-label="Play Heros Cover Album"]') as HTMLElement
    fireEvent.click(playButton)

    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('synced playlists appear in the shortcut grid and link to /synced-playlist/:id', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    const { useSyncedPlaylists } = await import('../lib/syncedPlaylistApi')

    const album = makeAlbum({ id: 'al1', name: 'Some Album', artist: 'Some Artist' })
    vi.mocked(useAlbums).mockReturnValue({ data: [album], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)

    const syncedPlaylist: SyncedPlaylist = {
      id: 'sp1',
      source: 'spotify',
      externalId: 'ext1',
      name: 'Classical',
      coverUrl: 'https://example.com/classical.jpg',
      syncEnabled: true,
      syncIntervalSec: 86400,
      autoDownload: false,
      lastSyncedAt: 0,
      trackCount: 5,
    }
    vi.mocked(useSyncedPlaylists).mockReturnValue({ data: [syncedPlaylist], isLoading: false, error: null } as unknown as UseQueryResult<SyncedPlaylist[], Error>)

    render(wrap(<Home />))

    // The synced playlist name appears in the shortcut grid
    expect(screen.getByRole('button', { name: 'Classical' })).toBeInTheDocument()

    // Clicking it navigates to /synced-playlist/sp1 (not /playlist/sp1)
    mockNavigate.mockClear()
    fireEvent.click(screen.getByRole('button', { name: 'Classical' }))
    expect(mockNavigate).toHaveBeenCalledWith('/synced-playlist/sp1')
  })
})
