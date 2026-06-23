import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Artist from './Artist'

// ---------------------------------------------------------------------------
// Module mocks — must be hoisted before any imports that pull these modules.
// ---------------------------------------------------------------------------

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

vi.mock('../lib/coverageApi', () => ({
  useArtistDetail: vi.fn(),
}))

vi.mock('../lib/coverageStore', () => ({
  useCoverageStream: vi.fn(),
}))

vi.mock('../lib/downloadApi', () => ({
  postBatchDownload: vi.fn().mockResolvedValue([]),
}))

// Mock downloadStore so we can control which jobs are active per test.
// Default: empty jobs map → no active downloads.
vi.mock('../lib/downloadStore', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  useDownloads: vi.fn((selector: (s: any) => unknown) => selector({ jobs: {} })),
}))

// Mock libraryRevisionStore (used by coverageStore; irrelevant in Artist tests).
vi.mock('../lib/libraryRevisionStore', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
import { useDownloads } from '../lib/downloadStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { useNavigate } from 'react-router-dom'

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
  beforeEach(() => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: STUB_DETAIL,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)

    vi.mocked(useCoverageStream).mockReturnValue(STUB_COVERAGE)

    vi.mocked(postBatchDownload).mockClear()

    // Default: no active downloads
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vi.mocked(useDownloads).mockImplementation((selector: (s: any) => unknown) => selector({ jobs: {} }))
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
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    wrapper(<Artist />)
    const dlBtn = screen.getByRole('button', { name: /download all missing/i })
    fireEvent.click(dlBtn)
    expect(confirmSpy).toHaveBeenCalledWith('Download 1 missing tracks?')
    expect(postBatchDownload).toHaveBeenCalledWith([MISSING_TRACK])
    confirmSpy.mockRestore()
  })

  it('"Download all missing" does NOT download when confirm is cancelled', () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    wrapper(<Artist />)
    const dlBtn = screen.getByRole('button', { name: /download all missing/i })
    fireEvent.click(dlBtn)
    expect(confirmSpy).toHaveBeenCalled()
    expect(postBatchDownload).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
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
})
