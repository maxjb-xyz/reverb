import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import Collection from './Collection'
import { useDownloads } from '../lib/downloadStore'
import { useAuthStore } from '../lib/authStore'
import type { DownloadJob, DiscographyAlbum } from '../lib/types'
import type { CollectionSummary, CollectionArtist } from '../lib/collectionApi'

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        {ui}
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function makeDiscographyAlbum(overrides: Partial<DiscographyAlbum> = {}): DiscographyAlbum {
  return {
    source: 'spotify',
    externalId: 'alb-1',
    name: 'Test Album',
    coverUrl: 'http://example.com/cover.jpg',
    year: 2021,
    kind: 'album',
    totalTracks: 10,
    ...overrides,
  }
}

function makeArtist(overrides: Partial<CollectionArtist> = {}): CollectionArtist {
  return {
    libraryArtistId: 'art-1',
    name: 'Test Artist',
    coverArtId: 'cov-1',
    source: 'spotify',
    externalArtistId: 'ext-art-1',
    ownedAlbums: 7,
    totalAlbums: 12,
    missingAlbums: [
      makeDiscographyAlbum({ externalId: 'missing-1', name: 'Missing Album 1' }),
      makeDiscographyAlbum({ externalId: 'missing-2', name: 'Missing Album 2' }),
    ],
    ...overrides,
  }
}

function job(id: string, status: DownloadJob['status'], extra: Partial<DownloadJob> = {}): DownloadJob {
  return {
    id,
    dedupKey: id,
    status,
    progress: 0,
    downloaderName: 'spotdl',
    priority: 0,
    attempts: 0,
    source: 'spotify',
    externalId: id,
    title: id,
    artist: 'A',
    playWhenReady: false,
    createdAt: 1,
    startedAt: 0,
    finishedAt: 0,
    ...extra,
  }
}

// ------------------------------------------------------------------
// Mocks
// ------------------------------------------------------------------

vi.mock('../lib/collectionApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/collectionApi')>()
  return {
    ...actual,
    useCollection: vi.fn(),
  }
})

vi.mock('../lib/downloadApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/downloadApi')>()
  return {
    ...actual,
    postDownload: vi.fn(),
  }
})

vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    coverUrl: vi.fn((id: string) => `/api/v1/cover/${id}`),
  }
})

vi.mock('../lib/authStore', () => ({
  useAuthStore: vi.fn((selector: (s: any) => unknown) => selector({ can: () => false })),
}))

// ------------------------------------------------------------------
// Tests
// ------------------------------------------------------------------

function setAuth(caps: string[]) {
  vi.mocked(useAuthStore).mockImplementation((selector: (s: any) => unknown) =>
    selector({ can: (cap: string) => caps.includes(cap) }),
  )
}

describe('Collection page', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {}, paused: false })
    vi.clearAllMocks()
    setAuth([])
  })

  it('renders artist name, album coverage label, and missing album ghost cards', async () => {
    const { useCollection } = await import('../lib/collectionApi')
    const artist = makeArtist({
      name: 'Radiohead',
      ownedAlbums: 7,
      totalAlbums: 12,
      missingAlbums: [
        makeDiscographyAlbum({ externalId: 'missing-1', name: 'Missing Album 1' }),
        makeDiscographyAlbum({ externalId: 'missing-2', name: 'Missing Album 2' }),
      ],
    })

    vi.mocked(useCollection).mockReturnValue({
      data: { artists: [artist], resolvedCount: 1, artistCount: 1 },
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    // Artist name should render
    expect(screen.getByText('Radiohead')).toBeInTheDocument()

    // Album coverage label: "7 of 12 albums"
    expect(screen.getByText('7 of 12 albums')).toBeInTheDocument()

    // Ghost cards for missing albums (use getAllByRole to find the main card buttons)
    const buttons = screen.getAllByRole('button', { name: /missing album/i })
    expect(buttons.length).toBeGreaterThanOrEqual(2)
  })

  it('renders empty state when artists array is empty', async () => {
    const { useCollection } = await import('../lib/collectionApi')
    vi.mocked(useCollection).mockReturnValue({
      data: { artists: [], resolvedCount: 0, artistCount: 0 },
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    expect(screen.getByText('No coverage yet')).toBeInTheDocument()
    expect(screen.getByText(/open an artist page to map their discography/i)).toBeInTheDocument()
  })

  it('shows progress ring when a download job is upserted and status is running', async () => {
    const { useCollection } = await import('../lib/collectionApi')
    const artist = makeArtist({
      name: 'Radiohead',
      ownedAlbums: 7,
      totalAlbums: 12,
      missingAlbums: [
        makeDiscographyAlbum({ externalId: 'missing-1', name: 'Missing Album 1' }),
      ],
    })

    vi.mocked(useCollection).mockReturnValue({
      data: { artists: [artist], resolvedCount: 1, artistCount: 1 },
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    // Get all cards (including the main card and download button)
    const cards = screen.getAllByRole('button', { name: /missing album 1/i })
    const card = cards[0] // The main MediaCard button

    // No progress ring initially in the cover div
    let coverDiv = card.querySelector('[data-testid="mediacard-cover"]')
    let svg = coverDiv?.querySelector('svg[role="img"]')
    expect(svg).not.toBeInTheDocument()

    // Upsert a download job matching the missing album with running status and 40% progress
    useDownloads.getState().upsert(
      job('job-1', 'running', {
        source: 'spotify',
        externalId: 'missing-1',
        progress: 40,
      })
    )

    // Wait for the component to re-render and show the progress ring
    await waitFor(() => {
      coverDiv = card.querySelector('[data-testid="mediacard-cover"]')
      svg = coverDiv?.querySelector('svg[role="img"]')
      expect(svg).toBeInTheDocument()
      // SVG should have aria-label with progress value
      expect(svg).toHaveAttribute('aria-label', expect.stringMatching(/40%/))
    })
  })

  it('displays loading skeleton while data is loading', async () => {
    const { useCollection } = await import('../lib/collectionApi')
    vi.mocked(useCollection).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows a download button on ghost cards when the user can auto_approve', async () => {
    setAuth(['auto_approve'])
    const { useCollection } = await import('../lib/collectionApi')
    const artist = makeArtist({
      name: 'Radiohead',
      missingAlbums: [
        makeDiscographyAlbum({ externalId: 'missing-1', name: 'Missing Album 1' }),
      ],
    })

    vi.mocked(useCollection).mockReturnValue({
      data: { artists: [artist], resolvedCount: 1, artistCount: 1 },
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    expect(screen.getByLabelText('Download Missing Album 1')).toBeInTheDocument()
  })

  it('hides the download button on ghost cards when the user cannot auto_approve', async () => {
    setAuth([])
    const { useCollection } = await import('../lib/collectionApi')
    const artist = makeArtist({
      name: 'Radiohead',
      missingAlbums: [
        makeDiscographyAlbum({ externalId: 'missing-1', name: 'Missing Album 1' }),
      ],
    })

    vi.mocked(useCollection).mockReturnValue({
      data: { artists: [artist], resolvedCount: 1, artistCount: 1 },
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<CollectionSummary, Error>)

    render(wrap(<Collection />))

    expect(screen.queryByLabelText(/^Download /)).toBeNull()
  })
})
