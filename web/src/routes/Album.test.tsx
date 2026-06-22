import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Album from './Album'
import { makeTrack } from '../test/factories'
import type { AlbumDetail, ExternalTrackRef } from '../lib/types'

// ── Player mock ────────────────────────────────────────────────────────────────
const mockPlayTrackList = vi.fn()
const mockToggleShuffle = vi.fn()

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: { playTrackList: typeof mockPlayTrackList; toggleShuffle: typeof mockToggleShuffle; shuffle: boolean; current: null }) => unknown) =>
    selector({ playTrackList: mockPlayTrackList, toggleShuffle: mockToggleShuffle, shuffle: false, current: null }),
}))

// ── coverageApi mock ───────────────────────────────────────────────────────────
// The component will switch to useAlbumDetail; mock the whole module
vi.mock('../lib/coverageApi', () => ({
  useAlbumDetail: vi.fn(),
}))

// ── postBatchDownload mock ────────────────────────────────────────────────────
const mockPostBatchDownload = vi.fn().mockResolvedValue([])
vi.mock('../lib/downloadApi', () => ({
  postBatchDownload: (...args: unknown[]) => mockPostBatchDownload(...args),
  postDownload: vi.fn().mockResolvedValue({}),
  retryDownload: vi.fn().mockResolvedValue({}),
  reqFromResult: vi.fn(),
}))

// ── DownloadAction stub ───────────────────────────────────────────────────────
// Mock the module so tests don't need to wire up adapters/download-store.
// Renders a simple "Download" button that exercises the right-slot render path.
vi.mock('../components/download/DownloadAction', () => ({
  DownloadAction: ({ result }: { result: { title: string } }) => (
    <button type="button" aria-label={`Download ${result.title}`}>Download</button>
  ),
}))

// ── react-router params ───────────────────────────────────────────────────────
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: () => ({ source: 'spotify', id: 'AL' }),
  }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────
const ownedTrack1 = makeTrack({ id: 'L1', title: 'Everything in Its Right Place', artist: 'Radiohead', durationMs: 1000, trackNumber: 1 })
const ownedTrack2 = makeTrack({ id: 'L2', title: 'Kid A', artist: 'Radiohead', durationMs: 2000, trackNumber: 2 })

const missingRef: ExternalTrackRef = {
  source: 'spotify',
  externalId: 'm1',
  title: 'Treefingers',
  artist: 'Radiohead',
  durationMs: 2000,
}

const partialAlbum: AlbumDetail = {
  source: 'spotify',
  id: 'AL',
  name: 'Kid A',
  artist: 'Radiohead',
  artistId: 'art1',
  year: 2000,
  totalCount: 3,
  ownedCount: 2,
  coverUrl: 'http://img/cover.jpg',
  tracks: [
    {
      state: 'full',
      libraryTrack: ownedTrack1,
      title: 'Everything in Its Right Place',
      artist: 'Radiohead',
      trackNumber: 1,
      durationMs: 1000,
    },
    {
      state: 'full',
      libraryTrack: ownedTrack2,
      title: 'Kid A',
      artist: 'Radiohead',
      trackNumber: 2,
      durationMs: 2000,
    },
    {
      state: 'none',
      externalRef: missingRef,
      title: 'Treefingers',
      artist: 'Radiohead',
      trackNumber: 3,
      durationMs: 2000,
    },
  ],
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function wrapper(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/album/spotify/AL']}>
        <Routes>
          <Route path="/album/:source/:id" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function renderLoaded() {
  const { useAlbumDetail } = await import('../lib/coverageApi')
  vi.mocked(useAlbumDetail).mockReturnValue({
    data: partialAlbum,
    isLoading: false,
    isError: false,
  } as ReturnType<typeof useAlbumDetail>)
  wrapper(<Album />)
  // Wait for the album heading specifically (not a track row)
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
}

// ── Tests ─────────────────────────────────────────────────────────────────────
describe('Album page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading skeleton while fetching', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    expect(screen.getByTestId('album-skeleton')).toBeInTheDocument()
  })

  it('renders all 3 track rows (2 owned + 1 missing)', async () => {
    await renderLoaded()
    expect(screen.getByText('Everything in Its Right Place')).toBeInTheDocument()
    // "Kid A" appears as both the album title (h1) and a track row — getAllByText is correct here
    expect(screen.getAllByText('Kid A').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Treefingers')).toBeInTheDocument()
  })

  it('header shows "2 of 3 in library" when ownedCount < totalCount', async () => {
    await renderLoaded()
    expect(screen.getByText(/2 of 3 in library/)).toBeInTheDocument()
  })

  it('header shows totalCount song count and year', async () => {
    await renderLoaded()
    expect(screen.getByText(/3 songs/)).toBeInTheDocument()
    expect(screen.getByText(/2000/)).toBeInTheDocument()
  })

  it('artist link routes to /artist/library/art1', async () => {
    await renderLoaded()
    const link = screen.getByRole('link', { name: 'Radiohead' })
    expect(link).toHaveAttribute('href', '/artist/library/art1')
  })

  it('Play button calls playTrackList with the 2 owned tracks only', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /play kid a/i }))
    expect(mockPlayTrackList).toHaveBeenCalledWith(
      [ownedTrack1, ownedTrack2],
      0,
    )
  })

  it('"Download missing · 1" button calls postBatchDownload with the missing externalRef', async () => {
    await renderLoaded()
    const btn = screen.getByRole('button', { name: /download missing/i })
    fireEvent.click(btn)
    expect(mockPostBatchDownload).toHaveBeenCalledWith([missingRef])
  })

  it('missing track row renders a download affordance', async () => {
    await renderLoaded()
    // The stubbed DownloadAction renders an aria-label "Download <title>"
    expect(screen.getByRole('button', { name: /download treefingers/i })).toBeInTheDocument()
  })

  it('owned rows are playable — clicking Track 1 calls playTrackList with ownedIndex 0', async () => {
    await renderLoaded()
    // "Everything in Its Right Place" is unique — click the track row
    fireEvent.click(screen.getByText('Everything in Its Right Place'))
    expect(mockPlayTrackList).toHaveBeenCalledWith([ownedTrack1, ownedTrack2], 0)
  })

  it('owned row receives coverSrc from coverUrl when libraryTrack has no coverArtId', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    const albumWithCoverUrl: AlbumDetail = {
      ...partialAlbum,
      tracks: [
        {
          ...partialAlbum.tracks[0], // state:'full', libraryTrack has coverArtId:''
          coverUrl: 'https://img/per-track-cover.jpg',
        },
        ...partialAlbum.tracks.slice(1),
      ],
    }
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: albumWithCoverUrl,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
    // The per-track coverUrl must appear as an img src (Cover renders an <img>)
    const imgs = document.querySelectorAll('img')
    const srcs = Array.from(imgs).map((img) => img.getAttribute('src'))
    expect(srcs).toContain('https://img/per-track-cover.jpg')
  })

  it('renders every track row even when a track has an unexpected state (pending, no externalRef)', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    const albumWithPending: AlbumDetail = {
      ...partialAlbum,
      totalCount: 4,
      ownedCount: 2,
      tracks: [
        ...partialAlbum.tracks,
        {
          state: 'pending',
          title: 'Motion Picture Soundtrack',
          artist: 'Radiohead',
          trackNumber: 4,
          durationMs: 3000,
        },
      ],
    }
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: albumWithPending,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
    expect(screen.getByText('Motion Picture Soundtrack')).toBeInTheDocument()
  })

  it('shows EmptyState when album not found', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText(/album not found/i)).toBeInTheDocument())
  })

  // Explicit guard: external (spotify) source error also shows EmptyState, not a crash.
  // (useParams returns source='spotify' for this test file, but naming it explicitly
  //  makes the graceful-degrade contract clear.)
  it('shows EmptyState (not a crash) for spotify source when isError=true', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    await waitFor(() => {
      expect(screen.getByText(/album not found/i)).toBeInTheDocument()
    })
    // Must not render any track rows or album header
    expect(screen.queryByTestId('album-skeleton')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Kid A' })).not.toBeInTheDocument()
  })

  describe('library-source album (all tracks owned — unchanged behavior)', () => {
    it('no "Download missing" button when ownedCount === totalCount', async () => {
      const { useAlbumDetail } = await import('../lib/coverageApi')
      const fullAlbum: AlbumDetail = {
        ...partialAlbum,
        source: 'library',
        totalCount: 2,
        ownedCount: 2,
        tracks: partialAlbum.tracks.slice(0, 2),
      }
      vi.mocked(useAlbumDetail).mockReturnValue({
        data: fullAlbum,
        isLoading: false,
        isError: false,
      } as ReturnType<typeof useAlbumDetail>)
      wrapper(<Album />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
      expect(screen.queryByRole('button', { name: /download missing/i })).not.toBeInTheDocument()
    })

    it('no "X of Y in library" annotation when fully owned', async () => {
      const { useAlbumDetail } = await import('../lib/coverageApi')
      const fullAlbum: AlbumDetail = {
        ...partialAlbum,
        source: 'library',
        totalCount: 2,
        ownedCount: 2,
        tracks: partialAlbum.tracks.slice(0, 2),
      }
      vi.mocked(useAlbumDetail).mockReturnValue({
        data: fullAlbum,
        isLoading: false,
        isError: false,
      } as ReturnType<typeof useAlbumDetail>)
      wrapper(<Album />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
      expect(screen.queryByText(/in library/)).not.toBeInTheDocument()
    })
  })
})
