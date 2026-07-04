import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Artist from './Artist'

// ---------------------------------------------------------------------------
// Module mocks — must be hoisted before any imports that pull these modules.
// ---------------------------------------------------------------------------

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

vi.mock('../lib/coverageApi', () => ({
  useArtistDetail: vi.fn(),
}))

vi.mock('../lib/statsApi', () => ({
  entity: vi.fn(async () => ({
    Plays: 0,
    MsPlayed: 0,
    FirstPlayed: 0,
    LastPlayed: 0,
    TopTracks: [],
  })),
}))

vi.mock('../lib/coverageStore', () => ({
  useCoverageStream: vi.fn(),
}))

vi.mock('../lib/downloadApi', () => ({
  postBatchDownload: vi.fn().mockResolvedValue([]),
}))

vi.mock('../lib/requestApi', () => ({
  postBatchRequest: vi.fn().mockResolvedValue({ created: 2, skipped: 0, quotaCapped: 0, requests: [] }),
}))

vi.mock('../lib/authStore', () => ({

  useAuthStore: vi.fn((selector: (s: any) => unknown) => selector({ can: () => false })),
}))

// Mock downloadStore so we can control which jobs are active per test.
// Default: empty jobs map → no active downloads.
vi.mock('../lib/downloadStore', () => ({

  useDownloads: vi.fn((selector: (s: any) => unknown) => selector({ jobs: {} })),
}))

// Mock libraryRevisionStore (used by coverageStore; irrelevant in Artist tests).
vi.mock('../lib/libraryRevisionStore', () => ({

  useLibraryRevision: vi.fn((selector: (s: any) => unknown) => selector({ revision: 0 })),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: vi.fn().mockReturnValue({ source: 'spotify', id: 'ar-radiohead' }),
    useNavigate: vi.fn().mockReturnValue(vi.fn()),
  }
})

import { useArtistDetail } from '../lib/coverageApi'
import { useCoverageStream } from '../lib/coverageStore'
import { postBatchDownload } from '../lib/downloadApi'
import { postBatchRequest } from '../lib/requestApi'
import { useDownloads } from '../lib/downloadStore'
import { useAuthStore } from '../lib/authStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { useNavigate } from 'react-router-dom'
import { useToastStore } from '../lib/toastStore'
import * as statsApi from '../lib/statsApi'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const STUB_DETAIL = {
  source: 'spotify',
  id: 'ar-radiohead',
  name: 'Radiohead',
  resolved: true,
  coverUrl: 'https://cdn.example.com/radiohead.jpg',
  albums: [
    {
      source: 'spotify',
      externalId: 'AL',
      name: 'Kid A',
      year: 2000,
      kind: 'album' as const,
      totalTracks: 10,
      coverUrl: 'https://cdn.example.com/kida.jpg',
    },
    {
      source: 'spotify',
      externalId: 'S1',
      name: 'Creep',
      year: 1992,
      kind: 'single' as const,
      totalTracks: 1,
      coverUrl: 'https://cdn.example.com/creep.jpg',
    },
  ],
}

const MISSING_TRACK = {
  source: 'spotify',
  externalId: 'm1',
  title: 'x',
  durationMs: 1000,
}

const STUB_COVERAGE = {
  AL: {
    source: 'spotify',
    externalAlbumId: 'AL',
    state: 'partial' as const,
    ownedCount: 7,
    totalCount: 10,
    missingTracks: [MISSING_TRACK],
  },
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function wrapper(ui: React.ReactElement) {
  return render(
    <MemoryRouter initialEntries={['/artist/spotify/ar-radiohead']}>
      <Routes>
        <Route path="/artist/:source/:id" element={ui} />
        <Route path="/album/:source/:id" element={<div data-testid="album-page" />} />
      </Routes>
    </MemoryRouter>,
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Artist page', () => {
  // Helper: configure can('request') / can('auto_approve') per-test
  function setAuth(caps: string[]) {
    vi.mocked(useAuthStore).mockImplementation((selector: (s: any) => unknown) =>
      selector({ can: (cap: string) => caps.includes(cap) }),
    )
  }

  beforeEach(() => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: STUB_DETAIL,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)

    vi.mocked(useCoverageStream).mockReturnValue(STUB_COVERAGE)

    vi.mocked(postBatchDownload).mockClear()
    vi.mocked(postBatchRequest).mockClear()

    // Default: no active downloads

    vi.mocked(useDownloads).mockImplementation((selector: (s: any) => unknown) => selector({ jobs: {} }))

    // Default: user without request permission
    setAuth([])
  })

  it('renders loading skeleton while fetching', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.getByTestId('artist-skeleton')).toBeInTheDocument()
  })

  it('renders both album cards', () => {
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Creep' })).toBeInTheDocument()
  })

  it('renders the partial coverage chip with 7/10 for Kid A', () => {
    wrapper(<Artist />)
    expect(screen.getByText('7/10')).toBeInTheDocument()
  })

  it('clicking "Singles & EPs" filter hides Kid A and shows Creep', () => {
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Singles & EPs' }))
    expect(screen.queryByRole('button', { name: 'Kid A' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Creep' })).toBeInTheDocument()
  })

  it('clicking "Albums" filter shows Kid A and hides Creep', () => {
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Albums' }))
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Creep' })).not.toBeInTheDocument()
  })

  it('"Download all missing" button calls postBatchDownload when confirmed', () => {
    setAuth(['auto_approve'])
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    wrapper(<Artist />)
    const dlBtn = screen.getByRole('button', { name: /download all missing/i })
    fireEvent.click(dlBtn)
    expect(confirmSpy).toHaveBeenCalledWith('Download 1 missing tracks?')
    expect(postBatchDownload).toHaveBeenCalledWith([MISSING_TRACK])
    confirmSpy.mockRestore()
  })

  it('"Download all missing" does NOT download when confirm is cancelled', () => {
    setAuth(['auto_approve'])
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    wrapper(<Artist />)
    const dlBtn = screen.getByRole('button', { name: /download all missing/i })
    fireEvent.click(dlBtn)
    expect(confirmSpy).toHaveBeenCalled()
    expect(postBatchDownload).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
  })

  // ── Acquisition-button gating (capability-driven, mutually exclusive) ────────

  it('auto_approve user sees "Download all missing" and NOT "Request all"', () => {
    setAuth(['auto_approve'])
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: /download all missing/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /request all/i })).not.toBeInTheDocument()
  })

  it('requester (request, no auto_approve) sees "Request all" and NOT "Download all missing"', () => {
    setAuth(['request'])
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: /request all/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
  })

  it('user with BOTH caps sees ONLY "Download all missing" (auto_approve branch wins)', () => {
    setAuth(['auto_approve', 'request'])
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: /download all missing/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /request all/i })).not.toBeInTheDocument()
  })

  it('user with NEITHER cap sees neither acquisition button', () => {
    setAuth([])
    wrapper(<Artist />)
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /request all/i })).not.toBeInTheDocument()
  })

  it('shows EmptyState when artist not found', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.getByText(/artist not found/i)).toBeInTheDocument()
  })

  it('does not render "Download all missing" in degrade mode (resolved=false)', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, resolved: false },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
  })

  it('unresolved artist: album cards still render (library-only mode, no crash)', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, resolved: false },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    // Both album cards should still be present — library-only render, no error screen
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Creep' })).toBeInTheDocument()
    expect(screen.queryByText(/artist not found/i)).not.toBeInTheDocument()
  })

  it('unresolved artist: useCoverageStream is called with enabled=false (no SSE stream opened)', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, resolved: false },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    // Third argument must be false so no SSE connection is established
    expect(vi.mocked(useCoverageStream)).toHaveBeenCalledWith(
      expect.any(String),
      expect.any(String),
      false,
    )
  })

  it('album with coverage state "none" renders no coverage chip at rest', () => {
    // AL has no entry in coverage → state will be 'pending' when resolved=true,
    // but here we pass a coverage map where AL has state 'none' to confirm CoverageChip returns null.
    const noneCoverage = {
      AL: {
        source: 'spotify',
        externalAlbumId: 'AL',
        state: 'none' as const,
        ownedCount: 0,
        totalCount: 10,
        missingTracks: [],
      },
    }
    vi.mocked(useCoverageStream).mockReturnValue(noneCoverage)
    wrapper(<Artist />)
    // The 'none' state chip must be absent (CoverageChip returns null for state='none')
    expect(screen.queryByTestId('coverage-full')).not.toBeInTheDocument()
    // Partial chip text "0/10" must also be absent
    expect(screen.queryByText('0/10')).not.toBeInTheDocument()
    // The card itself must still render
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
  })

  it('header wrapper has dynamic style.background when palette is present', () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [120, 80, 200], text: '#FFFFFF', scrim: false })
    wrapper(<Artist />)
    const heading = screen.getByRole('heading', { name: 'Radiohead' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    const bg = (gradientWrapper as HTMLElement).style.background
    expect(bg).toContain('linear-gradient')
    // Browser normalizes rgb(120 80 200 / 0.55) → rgba(120, 80, 200, 0.55)
    expect(bg).toMatch(/120/)
    expect(bg).toMatch(/80/)
    expect(bg).toMatch(/200/)
  })

  it('header wrapper has no inline style when palette is null', () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    wrapper(<Artist />)
    const heading = screen.getByRole('heading', { name: 'Radiohead' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    expect((gradientWrapper as HTMLElement).style.background).toBe('')
  })

  it('clicking an album with libraryAlbumId navigates to /album/library/:id', () => {
    const mockNavigate = vi.fn()
    vi.mocked(useNavigate).mockReturnValue(mockNavigate)
    // Override detail with a library-mapped album
    vi.mocked(useArtistDetail).mockReturnValue({
      data: {
        ...STUB_DETAIL,
        albums: [
          {
            source: 'spotify',
            externalId: 'AL',
            name: 'Kid A',
            year: 2000,
            kind: 'album' as const,
            totalTracks: 10,
            coverUrl: 'https://cdn.example.com/kida.jpg',
            libraryAlbumId: 'libAlbum1',
          },
        ],
      },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Kid A' }))
    expect(mockNavigate).toHaveBeenCalledWith('/album/library/libAlbum1')
  })

  it('renders coverUrl as the artist header image when coverArtId is absent', () => {
    // STUB_DETAIL has coverUrl set and no coverArtId → Cover should get the Spotify CDN URL
    wrapper(<Artist />)
    const coverImg = screen.getByRole('img', { name: 'Radiohead' })
    expect(coverImg).toHaveAttribute('src', 'https://cdn.example.com/radiohead.jpg')
  })

  it('clicking an album without libraryAlbumId navigates to /album/spotify/:externalId', () => {
    const mockNavigate = vi.fn()
    vi.mocked(useNavigate).mockReturnValue(mockNavigate)
    // Use default STUB_DETAIL — album 'AL' has no libraryAlbumId
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Kid A' }))
    expect(mockNavigate).toHaveBeenCalledWith('/album/spotify/AL')
  })

  it('shows a progress ring for an album card whose missing track has a running job', () => {
    // Simulate a running job for the missing track externalId 'm1' of album AL.
    const runningJob = {
      id: 'j-m1',
      dedupKey: 'dk-m1',
      status: 'running' as const,
      progress: 55,
      downloaderName: 'spotdl',
      priority: 0,
      attempts: 0,
      source: 'spotify',
      externalId: 'm1',
      playWhenReady: false,
      createdAt: 1,
      startedAt: 0,
      finishedAt: 0,
    }

    vi.mocked(useDownloads).mockImplementation((selector: any) => selector({ jobs: { 'j-m1': runningJob } }))
    wrapper(<Artist />)
    // The Kid A card should now show the progress ring (determinate, 55%).
    expect(screen.getByRole('img', { name: /55%/i })).toBeInTheDocument()
    // The plain download button should be gone.
    expect(screen.queryByRole('button', { name: /download kid a/i })).not.toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // "In your library" section
  // ---------------------------------------------------------------------------

  it('"In your library" section renders when libraryAlbums is non-empty', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: {
        ...STUB_DETAIL,
        libraryAlbums: [
          {
            source: 'spotify',
            externalId: 'ext-ok-computer',
            name: 'OK Computer',
            year: 1997,
            kind: 'album' as const,
            totalTracks: 12,
            coverUrl: 'https://cdn.example.com/okcomputer.jpg',
            libraryAlbumId: 'lib-ok-computer',
          },
        ],
      },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    expect(screen.getByTestId('library-albums-section')).toBeInTheDocument()
    expect(screen.getByText('In your library')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'OK Computer' })).toBeInTheDocument()
  })

  it('"In your library" card links to /album/library/:libraryAlbumId', () => {
    const mockNavigate = vi.fn()
    vi.mocked(useNavigate).mockReturnValue(mockNavigate)
    vi.mocked(useArtistDetail).mockReturnValue({
      data: {
        ...STUB_DETAIL,
        libraryAlbums: [
          {
            source: 'spotify',
            externalId: 'ext-ok-computer',
            name: 'OK Computer',
            year: 1997,
            kind: 'album' as const,
            totalTracks: 12,
            libraryAlbumId: 'lib-ok-computer',
          },
        ],
      },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'OK Computer' }))
    expect(mockNavigate).toHaveBeenCalledWith('/album/library/lib-ok-computer')
  })

  it('"In your library" card falls back to externalId in path when libraryAlbumId is absent', () => {
    const mockNavigate = vi.fn()
    vi.mocked(useNavigate).mockReturnValue(mockNavigate)
    vi.mocked(useArtistDetail).mockReturnValue({
      data: {
        ...STUB_DETAIL,
        libraryAlbums: [
          {
            source: 'spotify',
            externalId: 'ext-ok-computer',
            name: 'OK Computer',
            year: 1997,
            kind: 'album' as const,
            totalTracks: 12,
            // no libraryAlbumId
          },
        ],
      },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'OK Computer' }))
    expect(mockNavigate).toHaveBeenCalledWith('/album/library/ext-ok-computer')
  })

  it('"In your library" section is absent when libraryAlbums is empty', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, libraryAlbums: [] as import('../lib/types').DiscographyAlbum[] },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    expect(screen.queryByTestId('library-albums-section')).not.toBeInTheDocument()
    expect(screen.queryByText('In your library')).not.toBeInTheDocument()
  })

  it('"In your library" section is absent when libraryAlbums is undefined', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, libraryAlbums: undefined },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    wrapper(<Artist />)
    expect(screen.queryByTestId('library-albums-section')).not.toBeInTheDocument()
    expect(screen.queryByText('In your library')).not.toBeInTheDocument()
  })

  // ---------------------------------------------------------------------------
  // "Request all" button
  // ---------------------------------------------------------------------------

  it('user with request permission sees "Request all" button when discography has not-fully-owned albums', () => {
    setAuth(['request'])
    // STUB_COVERAGE: AL partial (7/10), S1 has no coverage → both not fully owned
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: /request all/i })).toBeInTheDocument()
  })

  it('user WITHOUT request permission does not see "Request all" button', () => {
    setAuth([]) // no 'request' cap
    wrapper(<Artist />)
    expect(screen.queryByRole('button', { name: /request all/i })).not.toBeInTheDocument()
  })

  it('"Request all" button is disabled/absent when every album is fully owned', () => {
    setAuth(['request'])
    // Override coverage so both albums are fully owned
    vi.mocked(useCoverageStream).mockReturnValue({
      AL: { source: 'spotify', externalAlbumId: 'AL', state: 'full' as const, ownedCount: 10, totalCount: 10, missingTracks: [] },
      S1: { source: 'spotify', externalAlbumId: 'S1', state: 'full' as const, ownedCount: 1, totalCount: 1, missingTracks: [] },
    })
    wrapper(<Artist />)
    // Button should be absent or disabled when nothing to request
    const btn = screen.queryByRole('button', { name: /request all/i })
    expect(!btn || (btn as HTMLButtonElement).disabled).toBe(true)
  })

  it('clicking "Request all" → disclosure dialog shows album count and artist name', () => {
    setAuth(['request'])
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: /request all/i }))
    // Dialog should appear with artist name and count of not-fully-owned albums
    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeInTheDocument()
    expect(dialog).toHaveTextContent(/radiohead/i)
    expect(dialog).toHaveTextContent(/2/)
  })

  it('confirming "Request all" disclosure calls postBatchRequest with one item per not-fully-owned album', async () => {
    setAuth(['request'])
    // STUB_COVERAGE: AL partial 7/10 → not owned; S1 has no coverage → not owned
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: /request all/i }))
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    // Give the promise a tick to resolve
    await Promise.resolve()
    expect(vi.mocked(postBatchRequest)).toHaveBeenCalledTimes(1)
    const [items] = vi.mocked(postBatchRequest).mock.calls[0]
    expect(items).toHaveLength(2)
    // Both items must be kind:'album'
    expect(items.every((i) => i.kind === 'album')).toBe(true)
    // Kid A
    const kidA = items.find((i) => i.externalId === 'AL')
    expect(kidA).toMatchObject({
      kind: 'album',
      source: 'spotify',
      externalId: 'AL',
      title: 'Kid A',
      album: 'Kid A',
      artist: 'Radiohead',
      trackCount: 10,
    })
    // Creep
    const creep = items.find((i) => i.externalId === 'S1')
    expect(creep).toMatchObject({
      kind: 'album',
      source: 'spotify',
      externalId: 'S1',
      title: 'Creep',
      album: 'Creep',
      artist: 'Radiohead',
      trackCount: 1,
    })
  })

  it('cancelling "Request all" disclosure does NOT call postBatchRequest', () => {
    setAuth(['request'])
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: /request all/i }))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(vi.mocked(postBatchRequest)).not.toHaveBeenCalled()
  })

  // ── Task 2: quotaCapped in batch toast ────────────────────────────────────

  it('quotaCapped > 0: success toast notes "N not requested (limit reached)"', async () => {
    setAuth(['request'])
    vi.mocked(postBatchRequest).mockResolvedValueOnce({
      created: 1,
      skipped: 0,
      quotaCapped: 2,
      requests: [],
    })
    useToastStore.setState({ toasts: [] })
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: /request all/i }))
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      const toasts = useToastStore.getState().toasts
      expect(toasts.some((t) =>
        t.kind === 'success' && /2 not requested \(limit reached\)/i.test(t.message),
      )).toBe(true)
    })
  })

  it('quotaCapped = 0: success toast does NOT mention "limit reached"', async () => {
    setAuth(['request'])
    vi.mocked(postBatchRequest).mockResolvedValueOnce({
      created: 2,
      skipped: 0,
      quotaCapped: 0,
      requests: [],
    })
    useToastStore.setState({ toasts: [] })
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: /request all/i }))
    fireEvent.click(screen.getByRole('button', { name: /confirm/i }))
    await waitFor(() => {
      const toasts = useToastStore.getState().toasts
      expect(toasts.some((t) => t.kind === 'success')).toBe(true)
      expect(toasts.some((t) => /limit reached/i.test(t.message))).toBe(false)
    })
  })

  // ── Task 13: Artist per-entity stat strip ──────────────────────────────────

  it('stat strip renders play count and listened time when artist has history', async () => {
    vi.mocked(statsApi.entity).mockResolvedValue({
      Plays: 42,
      MsPlayed: 9_000_000, // 2h 30m
      FirstPlayed: 1_700_000_000,
      LastPlayed: 1_750_000_000,
      TopTracks: [],
    })
    wrapper(<Artist />)

    await screen.findByText(/42 plays/i)
    expect(screen.getByText(/42 plays/i)).toBeInTheDocument()
    // msToHuman(9_000_000) = "2h 30m"
    expect(screen.getByText(/2h 30m/i)).toBeInTheDocument()
  })

  it('stat strip is hidden when artist has no play history (Plays === 0)', async () => {
    vi.mocked(statsApi.entity).mockResolvedValue({
      Plays: 0,
      MsPlayed: 0,
      FirstPlayed: 0,
      LastPlayed: 0,
      TopTracks: [],
    })
    wrapper(<Artist />)

    // Give async data time to load and settle
    await new Promise((r) => setTimeout(r, 10))
    expect(screen.queryByText(/plays/i)).not.toBeInTheDocument()
  })

  // ── Per-album-card download gating (Fix 1) ────────────────────────────────

  it('request-only user (no auto_approve) sees NO per-card "Download" button for a missing-coverage album', () => {
    setAuth(['request']) // request but NOT auto_approve
    // STUB_COVERAGE has AL with 1 missing track → hasMissing=true
    wrapper(<Artist />)
    // The per-card button label is "Download <albumTitle>" (aria-label from MediaCard)
    expect(screen.queryByRole('button', { name: /download kid a/i })).not.toBeInTheDocument()
  })

  it('auto_approve user sees a per-card "Download" button for a missing-coverage album', () => {
    setAuth(['auto_approve'])
    // STUB_COVERAGE has AL with 1 missing track → hasMissing=true
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: /download kid a/i })).toBeInTheDocument()
  })
})
