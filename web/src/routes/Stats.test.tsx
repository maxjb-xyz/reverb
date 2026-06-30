import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Stats from './Stats'
import type { SummaryStats, TopRow, RecentRow } from '../lib/statsApi'

// ── statsApi mock ─────────────────────────────────────────────────────────────
const mockSummary = vi.fn()
const mockTopTracks = vi.fn()
const mockTopArtists = vi.fn()
const mockTopAlbums = vi.fn()
const mockRecent = vi.fn()

vi.mock('../lib/statsApi', () => ({
  summary: (...args: unknown[]) => mockSummary(...args),
  topTracks: (...args: unknown[]) => mockTopTracks(...args),
  topArtists: (...args: unknown[]) => mockTopArtists(...args),
  topAlbums: (...args: unknown[]) => mockTopAlbums(...args),
  recent: (...args: unknown[]) => mockRecent(...args),
  timeline: vi.fn().mockResolvedValue([]),
  clock: vi.fn().mockResolvedValue([]),
}))

// ── libraryApi mock ───────────────────────────────────────────────────────────
vi.mock('../lib/libraryApi', () => ({
  coverUrl: (id: string, size = 300) => (id ? `/api/v1/cover/${id}?size=${size}` : ''),
  trackCoverUrl: vi.fn(),
}))

// ── react-router mocks ────────────────────────────────────────────────────────
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────
const SUMMARY: SummaryStats = {
  Plays: 42,
  DistinctTracks: 15,
  DistinctArtists: 5,
  DistinctAlbums: 3,
  MsPlayed: 9_000_000, // 2h 30m
}

const TOP_TRACKS: TopRow[] = [
  { CatalogID: 'cat-1', Title: 'Neon Night', Artist: 'Synthwave Inc', Album: 'Retrowave', Plays: 12, MsPlayed: 2_400_000 },
  { CatalogID: 'cat-2', Title: 'Digital Rain', Artist: 'Cyber Trio', Album: 'Matrix OST', Plays: 8, MsPlayed: 1_600_000 },
]

const TOP_ARTISTS: TopRow[] = [
  { CatalogID: '', Title: 'Synthwave Inc', Artist: '', Album: '', Plays: 20, MsPlayed: 4_000_000 },
]

const TOP_ALBUMS: TopRow[] = [
  { CatalogID: '', Title: 'Retrowave', Artist: 'Synthwave Inc', Album: '', Plays: 15, MsPlayed: 3_000_000 },
]

const RECENT: RecentRow[] = [
  { CatalogID: 'cat-1', Title: 'Neon Night', Artist: 'Synthwave Inc', Album: 'Retrowave', PlayedAt: Math.floor(Date.now() / 1000) - 120 },
  { CatalogID: 'cat-2', Title: 'Digital Rain', Artist: 'Cyber Trio', Album: 'Matrix OST', PlayedAt: Math.floor(Date.now() / 1000) - 3700 },
]

// ── Test helpers ──────────────────────────────────────────────────────────────
function makeClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 0 } },
  })
}

function renderStats() {
  const client = makeClient()
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <Stats />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Stats page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSummary.mockResolvedValue(SUMMARY)
    mockTopTracks.mockResolvedValue(TOP_TRACKS)
    mockTopArtists.mockResolvedValue(TOP_ARTISTS)
    mockTopAlbums.mockResolvedValue(TOP_ALBUMS)
    mockRecent.mockResolvedValue(RECENT)
  })

  // ── Summary cards ─────────────────────────────────────────────────────────

  it('renders the page heading', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByRole('heading', { name: /stats/i })).toBeInTheDocument())
  })

  it('renders summary card: songs played count', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByText('42')).toBeInTheDocument())
  })

  it('renders summary card: time listened formatted as hours + minutes', async () => {
    renderStats()
    // 9_000_000 ms = 150 min = 2h 30m
    await waitFor(() => expect(screen.getByText('2h 30m')).toBeInTheDocument())
  })

  it('renders summary card: distinct tracks count', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByText('15')).toBeInTheDocument())
  })

  it('renders summary card: distinct artists count', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByText('5')).toBeInTheDocument())
  })

  it('renders summary card: distinct albums count', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByText('3')).toBeInTheDocument())
  })

  // ── Top tracks list ───────────────────────────────────────────────────────

  it('renders top track title', async () => {
    renderStats()
    await waitFor(() => {
      const els = screen.getAllByText('Neon Night')
      expect(els.length).toBeGreaterThan(0)
    })
  })

  it('renders rank number for top track', async () => {
    renderStats()
    await waitFor(() => {
      const els = screen.getAllByText('1')
      expect(els.length).toBeGreaterThan(0)
    })
  })

  it('renders top track cover img using CatalogID', async () => {
    renderStats()
    await waitFor(() => {
      const imgs = screen.getAllByRole('img')
      const cover = imgs.find((img) => img.getAttribute('src')?.includes('cat-1'))
      expect(cover).toBeTruthy()
    })
  })

  it('renders top track play count and time', async () => {
    renderStats()
    // Track 1: 12 plays, 2400000 ms = 40m
    await waitFor(() => expect(screen.getByText(/12 plays/i)).toBeInTheDocument())
  })

  // ── Top artists list ──────────────────────────────────────────────────────

  it('renders top artist name', async () => {
    renderStats()
    await waitFor(() => {
      const els = screen.getAllByText('Synthwave Inc')
      expect(els.length).toBeGreaterThan(0)
    })
  })

  // ── Top albums list ───────────────────────────────────────────────────────

  it('renders top album name', async () => {
    renderStats()
    await waitFor(() => expect(screen.getByText('Retrowave')).toBeInTheDocument())
  })

  // ── Recently played ───────────────────────────────────────────────────────

  it('renders recently-played section with track title', async () => {
    renderStats()
    await waitFor(() => {
      // Digital Rain appears in both top-tracks list AND recently-played
      const els = screen.getAllByText('Digital Rain')
      expect(els.length).toBeGreaterThan(0)
    })
  })

  it('renders relative time for recently-played row', async () => {
    renderStats()
    // PlayedAt = now - 120s => ~"2m ago"
    await waitFor(() => expect(screen.getByText(/2m ago/i)).toBeInTheDocument())
  })

  // ── Range selector re-fetch ───────────────────────────────────────────────

  it('refetches when range changes: summary is called with new range params', async () => {
    renderStats()
    await waitFor(() => expect(mockSummary).toHaveBeenCalledTimes(1))

    const callsBefore = mockSummary.mock.calls.length
    const fromBefore = mockSummary.mock.calls[0][0].from

    // Click "7d" chip — changes range from 30d default
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^7d$/i }))
    })

    await waitFor(() => {
      expect(mockSummary.mock.calls.length).toBeGreaterThan(callsBefore)
      // The new call has a different (more recent) "from" timestamp
      const lastFrom = mockSummary.mock.calls[mockSummary.mock.calls.length - 1][0].from
      expect(lastFrom).toBeGreaterThan(fromBefore)
    })
  })

  it('refetches topTracks when range changes to "7d"', async () => {
    renderStats()
    await waitFor(() => expect(mockTopTracks).toHaveBeenCalledTimes(1))

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^7d$/i }))
    })

    await waitFor(() => {
      expect(mockTopTracks.mock.calls.length).toBeGreaterThan(1)
    })
  })

  // ── Empty state ───────────────────────────────────────────────────────────

  it('shows empty state when summary has no plays', async () => {
    mockSummary.mockResolvedValue({ ...SUMMARY, Plays: 0 })
    mockTopTracks.mockResolvedValue([])
    mockTopArtists.mockResolvedValue([])
    mockTopAlbums.mockResolvedValue([])
    mockRecent.mockResolvedValue([])
    renderStats()
    await waitFor(() =>
      expect(screen.getByText(/no listening history/i)).toBeInTheDocument()
    )
  })
})
