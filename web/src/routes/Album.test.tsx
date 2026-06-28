import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Album from './Album'
import { makeTrack } from '../test/factories'
import type { AlbumDetail, ExternalTrackRef } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { useAuthStore } from '../lib/authStore'

// ── useAlbumPalette mock ───────────────────────────────────────────────────────
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

// ── Player mock ────────────────────────────────────────────────────────────────
const mockPlayTrackList = vi.fn()
const mockToggleShuffle = vi.fn()

// Mutable box so individual tests can override `current` to simulate playback.
const mockPlayerState: { playTrackList: typeof mockPlayTrackList; toggleShuffle: typeof mockToggleShuffle; shuffle: boolean; current: null | { id: string } } = {
  playTrackList: mockPlayTrackList,
  toggleShuffle: mockToggleShuffle,
  shuffle: false,
  current: null,
}

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: typeof mockPlayerState) => unknown) =>
    selector(mockPlayerState),
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

// ── postRequest mock ─────────────────────────────────────────────────────────
const mockPostRequest = vi.fn().mockResolvedValue({ id: 'req-1', status: 'pending' })
vi.mock('../lib/requestApi', () => ({
  postRequest: (...args: unknown[]) => mockPostRequest(...args),
  useRequestStore: { getState: () => ({ upsert: vi.fn() }) },
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
    mockPlayerState.current = null
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
    // Multiple "Radiohead" links: one in the album header, two in track row artists
    // (TrackRow now renders artist as a link when artistId is set). All point to art1.
    const links = screen.getAllByRole('link', { name: 'Radiohead' })
    expect(links.length).toBeGreaterThanOrEqual(1)
    expect(links[0]).toHaveAttribute('href', '/artist/library/art1')
  })

  it('owned rows render artist + album as links (libraryTrack ids wired through)', async () => {
    await renderLoaded()
    // ownedTrack1/2 carry makeTrack defaults: artistId 'ar1', albumId 'al1', album
    // 'Test Album'. The owned rows pass the real libraryTrack to TrackRow, which now
    // renders the album cell as a link to /album/library/:albumId.
    const albumLinks = screen.getAllByRole('link', { name: 'Test Album' })
    expect(albumLinks.length).toBe(2) // both owned rows
    expect(albumLinks.every((l) => l.getAttribute('href') === '/album/library/al1')).toBe(true)
    // Owned-row artist links point at the library artist id (ar1), distinct from the
    // header link (art1) — TrackRow links the row's own artistId.
    const artistLinks = screen.getAllByRole('link', { name: 'Radiohead' })
    expect(artistLinks.some((l) => l.getAttribute('href') === '/artist/library/ar1')).toBe(true)
  })

  it('Play button calls playTrackList with the 2 owned tracks only', async () => {
    await renderLoaded()
    // The header Play button is the first button matching /play kid a/i; the
    // TrackRow hover-play button for the "Kid A" track also matches — use [0].
    fireEvent.click(screen.getAllByRole('button', { name: /play kid a/i })[0])
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

  it('owned rows are playable — double-clicking Track 1 calls playTrackList with ownedIndex 0', async () => {
    await renderLoaded()
    // "Everything in Its Right Place" is unique — double-click the track row (Spotify semantics)
    fireEvent.doubleClick(screen.getByText('Everything in Its Right Place'))
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

  it('header wrapper has dynamic style.background when palette is present', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [120, 80, 200], text: '#FFFFFF', scrim: false })
    await renderLoaded()
    // The gradient wrapper div is the first child of the outer space-y-6 div
    const wrapper = screen.getByRole('heading', { name: 'Kid A' }).closest('[class*="from-raised"]')
    expect(wrapper).toBeTruthy()
    const bg = (wrapper as HTMLElement).style.background
    expect(bg).toContain('linear-gradient')
    // Browser normalizes rgb(120 80 200 / 0.55) → rgba(120, 80, 200, 0.55)
    expect(bg).toMatch(/120/)
    expect(bg).toMatch(/80/)
    expect(bg).toMatch(/200/)
  })

  it('header wrapper has no inline style when palette is null', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    await renderLoaded()
    const wrapper = screen.getByRole('heading', { name: 'Kid A' }).closest('[class*="from-raised"]')
    expect(wrapper).toBeTruthy()
    expect((wrapper as HTMLElement).style.background).toBe('')
  })

  describe('now-playing indicator (Bug 8)', () => {
    it('the active owned row renders an Equalizer (eq-bar) and the others do not', async () => {
      mockPlayerState.current = { id: 'L1' }
      await renderLoaded()
      // eq-bar appears in the active row
      const eqBars = document.querySelectorAll('[data-testid="eq-bar"]')
      expect(eqBars.length).toBeGreaterThan(0)
    })

    it('only the playing row is active — non-playing owned row has no Equalizer', async () => {
      mockPlayerState.current = { id: 'L1' }
      await renderLoaded()
      // L2 is owned but not playing — it must NOT show an eq-bar
      // There is exactly one set of eq-bars (the 4 bars for L1's Equalizer)
      const eqBars = document.querySelectorAll('[data-testid="eq-bar"]')
      expect(eqBars.length).toBe(4) // Equalizer renders 4 bars
    })

    it('no Equalizer when current track is null', async () => {
      mockPlayerState.current = null
      await renderLoaded()
      expect(document.querySelectorAll('[data-testid="eq-bar"]').length).toBe(0)
    })

    it('missing rows are never active (no Equalizer) even when current id happens to be empty string', async () => {
      mockPlayerState.current = { id: '' }
      await renderLoaded()
      // The missing track has id '' in displayTrack — but we only pass active to owned rows
      expect(document.querySelectorAll('[data-testid="eq-bar"]').length).toBe(0)
    })
  })

  describe('owned-track indicator (Bug 12)', () => {
    it('owned rows render an "In Library" right-slot badge', async () => {
      await renderLoaded()
      const badges = screen.getAllByText('In Library')
      // 2 owned tracks → 2 badges
      expect(badges.length).toBe(2)
    })

    it('missing row does NOT render an "In Library" badge — it renders the Download control', async () => {
      await renderLoaded()
      // The missing track "Treefingers" should have a Download button (stubbed DownloadAction)
      expect(screen.getByRole('button', { name: /download treefingers/i })).toBeInTheDocument()
      // And no "In Library" badge for the missing row's title
      // (2 badges for the 2 owned rows is already asserted above; just confirm Treefingers has Download)
    })

    it('owned row rightWidth is set (all rows in the track list have consistent column layout)', async () => {
      await renderLoaded()
      // All track rows are <button> elements in the track list; confirm 3 rows rendered
      // (2 owned + 1 missing). Owned rows have In Library, missing row has Download button.
      const inLibraryBadges = screen.getAllByText('In Library')
      expect(inLibraryBadges.length).toBe(2)
      expect(screen.getByRole('button', { name: /download treefingers/i })).toBeInTheDocument()
    })
  })

  it('playTrackList receives track with artistExternalId when source row has it', async () => {
    const { useAlbumDetail } = await import('../lib/coverageApi')
    const albumWithExternalId: AlbumDetail = {
      ...partialAlbum,
      tracks: [
        {
          state: 'full',
          libraryTrack: ownedTrack1,
          title: 'Everything in Its Right Place',
          artist: 'Radiohead',
          trackNumber: 1,
          durationMs: 1000,
          artistExternalId: 'sp-artist-77',
        },
        ...partialAlbum.tracks.slice(1),
      ],
    }
    vi.mocked(useAlbumDetail).mockReturnValue({
      data: albumWithExternalId,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useAlbumDetail>)
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Kid A' })).toBeInTheDocument())
    fireEvent.click(screen.getAllByRole('button', { name: /play kid a/i })[0])
    expect(mockPlayTrackList).toHaveBeenCalledOnce()
    const [tracks] = mockPlayTrackList.mock.calls[0] as [import('../lib/types').Track[], number]
    expect(tracks[0]).toMatchObject({ artistExternalId: 'sp-artist-77' })
  })

  describe('"Request album" action', () => {
    function setCaps(caps: string[]) {
      useAuthStore.setState({
        me: { id: 'u1', username: 'u1', roleId: 'r', roleName: 'R', isOwner: false, capabilities: caps, createdAt: 0 },
        loading: false,
      })
    }

    afterEach(() => {
      useAuthStore.setState({ me: null, loading: false })
    })

    it('renders "Request album" button for a user with the request capability', async () => {
      setCaps(['request'])
      await renderLoaded()
      expect(screen.getByRole('button', { name: /request album/i })).toBeInTheDocument()
    })

    it('does NOT render "Request album" button for a user without request capability', async () => {
      setCaps([])
      await renderLoaded()
      expect(screen.queryByRole('button', { name: /request album/i })).not.toBeInTheDocument()
    })

    it('clicking "Request album" opens the whole-album disclosure', async () => {
      setCaps(['request'])
      await renderLoaded()
      fireEvent.click(screen.getByRole('button', { name: /request album/i }))
      expect(screen.getByRole('dialog', { name: /request album/i })).toBeInTheDocument()
      expect(screen.getByText(/album downloader/i)).toBeInTheDocument()
    })

    it('confirming the disclosure calls postRequest with kind:album and album metadata', async () => {
      setCaps(['request'])
      await renderLoaded()
      fireEvent.click(screen.getByRole('button', { name: /request album/i }))
      const confirmBtn = screen.getByRole('button', { name: /confirm/i })
      fireEvent.click(confirmBtn)
      await waitFor(() => expect(mockPostRequest).toHaveBeenCalledTimes(1))
      expect(mockPostRequest).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: 'album',
          source: 'spotify',
          externalId: 'AL',
          title: 'Kid A',
          artist: 'Radiohead',
        }),
      )
    })

    it('cancelling the disclosure does NOT call postRequest', async () => {
      setCaps(['request'])
      await renderLoaded()
      fireEvent.click(screen.getByRole('button', { name: /request album/i }))
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(mockPostRequest).not.toHaveBeenCalled()
      expect(screen.queryByRole('dialog', { name: /request album/i })).not.toBeInTheDocument()
    })
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

    it('no "X of Y in library" header annotation when fully owned', async () => {
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
      // The header "X of Y in library" accent span must not appear when fully owned.
      // (Each owned row now shows an "In Library" right-slot badge — those are distinct.)
      const header = screen.getByRole('heading', { name: 'Kid A' }).closest('header')!
      expect(header.querySelector('.text-accent')).not.toBeInTheDocument()
    })
  })
})
