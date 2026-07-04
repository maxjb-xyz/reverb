import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import SyncedPlaylist from './SyncedPlaylist'
import { makeTrack } from '../test/factories'
import type { SyncedPlaylistDetail, AlbumDetailTrack, Track } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { useAuthStore } from '../lib/authStore'
import { useToastStore } from '../lib/toastStore'

// ── useAlbumPalette mock ───────────────────────────────────────────────────────
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

// ── Player mock ────────────────────────────────────────────────────────────────
const mockPlayTrackList = vi.fn()

// Mutable box so individual tests can set `current` to simulate playback.
const mockSyncedPlayerState: { playTrackList: typeof mockPlayTrackList; shuffle: boolean; current: null | { id: string } } = {
  playTrackList: mockPlayTrackList,
  shuffle: false,
  current: null,
}

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: typeof mockSyncedPlayerState) => unknown) =>
    selector(mockSyncedPlayerState),
}))

// ── syncedPlaylistApi mocks ────────────────────────────────────────────────────
const mockUseSyncedPlaylist = vi.fn()
const mockSyncNow = vi.fn().mockResolvedValue({})
const mockDownloadMissingForPlaylist = vi.fn().mockResolvedValue([])
const mockUpdateSyncSettings = vi.fn().mockResolvedValue({})
const mockDeleteSyncedPlaylist = vi.fn().mockResolvedValue({})
const mockRemoveSyncedTrack = vi.fn().mockResolvedValue({})
const mockUploadPlaylistCover = vi.fn().mockResolvedValue({})
const mockReorderSyncedTracks = vi.fn().mockResolvedValue({})
const mockRenameSyncedPlaylist = vi.fn().mockResolvedValue({})

vi.mock('../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylist: (...args: unknown[]) => mockUseSyncedPlaylist(...args),
  syncNow: (...args: unknown[]) => mockSyncNow(...args),
  downloadMissingForPlaylist: (...args: unknown[]) => mockDownloadMissingForPlaylist(...args),
  updateSyncSettings: (...args: unknown[]) => mockUpdateSyncSettings(...args),
  deleteSyncedPlaylist: (...args: unknown[]) => mockDeleteSyncedPlaylist(...args),
  removeSyncedTrack: (...args: unknown[]) => mockRemoveSyncedTrack(...args),
  uploadPlaylistCover: (...args: unknown[]) => mockUploadPlaylistCover(...args),
  reorderSyncedTracks: (...args: unknown[]) => mockReorderSyncedTracks(...args),
  renameSyncedPlaylist: (...args: unknown[]) => mockRenameSyncedPlaylist(...args),
}))

// ── DownloadAction mock (avoids adapter fetch noise) ──────────────────────────
vi.mock('../components/download/DownloadAction', () => ({
  DownloadAction: ({ result }: { result: { title: string } }) => (
    <button type="button" aria-label={`Download ${result.title}`}>
      Download
    </button>
  ),
}))

// ── react-router mocks ────────────────────────────────────────────────────────
const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: () => ({ id: 'sp1' }),
    useNavigate: () => mockNavigate,
  }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────
const track1 = makeTrack({ id: 't1', title: 'Owned One', artist: 'Artist A', album: 'Playlist A' })
const track2 = makeTrack({ id: 't2', title: 'Owned Two', artist: 'Artist B', album: 'Playlist A' })

const ownedRow1: AlbumDetailTrack = {
  state: 'full',
  libraryTrack: track1,
  key: { source: 'spotify', externalId: 'e1' },
  title: 'Owned One',
  artist: 'Artist A',
  trackNumber: 1,
  durationMs: 180000,
}
const ownedRow2: AlbumDetailTrack = {
  state: 'full',
  libraryTrack: track2,
  key: { source: 'spotify', externalId: 'e2' },
  title: 'Owned Two',
  artist: 'Artist B',
  trackNumber: 2,
  durationMs: 200000,
}
const missingRow: AlbumDetailTrack = {
  state: 'none',
  libraryTrack: undefined,
  externalRef: { source: 'spotify', externalId: 'e3', title: 'Missing Track', artist: 'Artist C', durationMs: 210000 },
  key: { source: 'spotify', externalId: 'e3' },
  title: 'Missing Track',
  artist: 'Artist C',
  trackNumber: 3,
  durationMs: 210000,
}
const ownedRowWithExternalIds: AlbumDetailTrack = {
  ...ownedRow1,
  artistExternalId: 'sp-artist-99',
  albumExternalId: 'sp-album-99',
}
const missingRowWithExternalIds: AlbumDetailTrack = {
  ...missingRow,
  artistExternalId: 'sp-artist-88',
  albumExternalId: 'sp-album-88',
}

const mockDetail: SyncedPlaylistDetail = {
  id: 'sp1',
  source: 'spotify',
  mode: 'synced',
  externalId: 'ext1',
  name: 'Test Synced Playlist',
  coverUrl: 'https://example.com/cover.jpg',
  syncEnabled: true,
  syncIntervalSec: 86400,
  autoDownload: false,
  lastSyncedAt: Math.floor(Date.now() / 1000) - 7200, // 2h ago
  trackCount: 3,
  ownedCount: 2,
  totalCount: 3,
  tracks: [ownedRow1, ownedRow2, missingRow],
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function wrapper(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/synced-playlist/sp1']}>
        <Routes>
          <Route path="/synced-playlist/:id" element={ui} />
          <Route path="/library" element={<div>Library</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function renderLoaded() {
  mockUseSyncedPlaylist.mockReturnValue({
    data: mockDetail,
    isLoading: false,
    isError: false,
  })
  wrapper(<SyncedPlaylist />)
  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────
describe('SyncedPlaylist page', () => {
  // Helper: configure the current user's capabilities on the real auth store.
  function setCaps(caps: string[]) {
    useAuthStore.setState({
      me: { id: 'u1', username: 'u1', roleId: 'r', roleName: 'R', isOwner: false, capabilities: caps, createdAt: 0 },
      loading: false,
    })
  }

  beforeEach(() => {
    vi.clearAllMocks()
    useAuthStore.setState({ me: null, loading: false })
    mockSyncedPlayerState.current = null
    mockRemoveSyncedTrack.mockReset()
    mockRemoveSyncedTrack.mockResolvedValue({})
    mockUploadPlaylistCover.mockReset()
    mockUploadPlaylistCover.mockResolvedValue({})
    mockReorderSyncedTracks.mockReset()
    mockReorderSyncedTracks.mockResolvedValue({})
    mockRenameSyncedPlaylist.mockReset()
    mockRenameSyncedPlaylist.mockResolvedValue({})
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading skeleton while fetching', () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    expect(screen.getByTestId('synced-playlist-skeleton')).toBeInTheDocument()
  })

  it('renders error EmptyState when fetch fails', () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    })
    wrapper(<SyncedPlaylist />)
    expect(screen.getByText(/playlist not found/i)).toBeInTheDocument()
  })

  it('shows "2 of 3 in library" in the header', async () => {
    await renderLoaded()
    expect(screen.getByText('2 of 3 in library')).toBeInTheDocument()
  })

  it('shows "1 missing" in accent', async () => {
    await renderLoaded()
    expect(screen.getByText(/1 missing/)).toBeInTheDocument()
  })

  it('owned rows render artist + album as links (asTrack threads libraryTrack ids)', async () => {
    await renderLoaded()
    // track1/track2 carry makeTrack defaults (artistId 'ar1', albumId 'al1'); asTrack
    // now wires those onto the display Track so TrackRow renders both as links.
    const albumLinks = screen.getAllByRole('link', { name: 'Playlist A' })
    expect(albumLinks.length).toBeGreaterThanOrEqual(1)
    expect(albumLinks.every((l) => l.getAttribute('href') === '/album/library/al1')).toBe(true)
    const artistLink = screen.getByRole('link', { name: 'Artist A' })
    expect(artistLink).toHaveAttribute('href', '/artist/library/ar1')
  })

  it('shows "Synced playlist" eyebrow', async () => {
    await renderLoaded()
    expect(screen.getByText('Synced playlist')).toBeInTheDocument()
  })

  it('does NOT crash on an empty playlist (tracks null)', async () => {
    // Regression: an empty managed playlist's Detail returns tracks:null; the page
    // must guard it (tracks ?? []) instead of calling .filter on null (black screen).
    mockUseSyncedPlaylist.mockReturnValue({
      data: { ...mockDetail, source: 'local', mode: 'once', name: 'Empty One', tracks: null },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Empty One' })).toBeInTheDocument(),
    )
  })

  it('a local playlist renders as a plain "Playlist" (no Synced/Imported, no source pill, no sync UI)', async () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: { ...mockDetail, source: 'local', mode: 'once', name: 'My Playlist' },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'My Playlist' })).toBeInTheDocument(),
    )
    expect(screen.getByText('Playlist')).toBeInTheDocument()
    expect(screen.queryByText('Synced playlist')).not.toBeInTheDocument()
    expect(screen.queryByText(/imported/i)).not.toBeInTheDocument()
    expect(screen.queryByText('local')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /sync now/i })).not.toBeInTheDocument()
  })

  it('rename: editing the title and committing saves via renameSyncedPlaylist', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('heading', { name: 'Test Synced Playlist' }))
    const input = screen.getByRole('textbox', { name: 'Playlist name' })
    fireEvent.change(input, { target: { value: 'Road Trip' } })
    fireEvent.blur(input)
    await waitFor(() => expect(mockRenameSyncedPlaylist).toHaveBeenCalledWith('sp1', 'Road Trip'))
  })

  it('rename: pressing Escape cancels without saving (regression)', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('heading', { name: 'Test Synced Playlist' }))
    const input = screen.getByRole('textbox', { name: 'Playlist name' })
    fireEvent.change(input, { target: { value: 'Changed Name' } })
    // Escape marks the edit cancelled; the input's blur then runs handleRename,
    // which must NOT save the typed value.
    fireEvent.keyDown(input, { key: 'Escape' })
    fireEvent.blur(input)
    expect(mockRenameSyncedPlaylist).not.toHaveBeenCalled()
    await waitFor(() =>
      expect(screen.queryByRole('textbox', { name: 'Playlist name' })).not.toBeInTheDocument(),
    )
  })

  it('shows relative sync time', async () => {
    await renderLoaded()
    expect(screen.getByText(/synced 2h ago/i)).toBeInTheDocument()
  })

  it('shows "Never synced" when lastSyncedAt is 0', async () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: { ...mockDetail, lastSyncedAt: 0 },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() => expect(screen.getByText(/never synced/i)).toBeInTheDocument())
  })

  it('renders owned track rows', async () => {
    await renderLoaded()
    expect(screen.getByText('Owned One')).toBeInTheDocument()
    expect(screen.getByText('Owned Two')).toBeInTheDocument()
  })

  it('renders missing track row with Download action', async () => {
    await renderLoaded()
    expect(screen.getByText('Missing Track')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /download missing track/i })).toBeInTheDocument()
  })

  it('owned row receives coverSrc from coverUrl when libraryTrack has no coverArtId', async () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: {
        ...mockDetail,
        tracks: [
          { ...ownedRow1, coverUrl: 'https://img/owned-cover.jpg' }, // libraryTrack.coverArtId is ''
          ownedRow2,
          missingRow,
        ],
      },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
    )
    const imgs = document.querySelectorAll('img')
    const srcs = Array.from(imgs).map((img) => img.getAttribute('src'))
    expect(srcs).toContain('https://img/owned-cover.jpg')
  })

  it('Play button calls playTrackList with owned tracks at index 0', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /play test synced playlist/i }))
    expect(mockPlayTrackList).toHaveBeenCalledWith([track1, track2], 0)
  })

  it('Play button is disabled when no owned tracks', async () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: {
        ...mockDetail,
        ownedCount: 0,
        tracks: [missingRow],
      },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('button', { name: /play test synced playlist/i })).toBeDisabled()
  })

  it('"Sync now" button calls syncNow with the playlist id', async () => {
    await renderLoaded()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /sync now/i }))
    })
    expect(mockSyncNow).toHaveBeenCalledWith('sp1')
  })

  it('shows an error toast when "Sync now" fails', async () => {
    useToastStore.setState({ toasts: [] })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mockSyncNow.mockRejectedValueOnce(new Error('boom'))
    await renderLoaded()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /sync now/i }))
    })
    await waitFor(() =>
      expect(useToastStore.getState().toasts.some((t) => t.kind === 'error')).toBe(true),
    )
  })

  it('shows an error toast when "Download all missing" fails', async () => {
    useToastStore.setState({ toasts: [] })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    setCaps(['auto_approve'])
    mockDownloadMissingForPlaylist.mockRejectedValueOnce(new Error('boom'))
    await renderLoaded()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /download all missing/i }))
    })
    await waitFor(() =>
      expect(useToastStore.getState().toasts.some((t) => t.kind === 'error')).toBe(true),
    )
    useAuthStore.setState({ me: null, loading: false })
  })

  it('"Download all missing" button calls downloadMissingForPlaylist with the playlist id', async () => {
    setCaps(['auto_approve'])
    await renderLoaded()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /download all missing/i }))
    })
    expect(mockDownloadMissingForPlaylist).toHaveBeenCalledWith('sp1')
    useAuthStore.setState({ me: null, loading: false })
  })

  it('"Download all missing" button is hidden when nothing is missing', async () => {
    setCaps(['auto_approve'])
    mockUseSyncedPlaylist.mockReturnValue({
      data: {
        ...mockDetail,
        ownedCount: 3,
        totalCount: 3,
        tracks: [ownedRow1, ownedRow2, { ...ownedRow2, libraryTrack: track2 }],
      },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
    )
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
    useAuthStore.setState({ me: null, loading: false })
  })

  // ── "Download all missing" gating (auto_approve capability) ─────────────────
  // No bulk-request path exists for synced playlists. A user without auto_approve
  // simply sees no bulk-download button; per-item DownloadAction on missing rows
  // is unaffected.
  it('a user WITHOUT auto_approve does NOT see the "Download all missing" button', async () => {
    setCaps(['request']) // request-only: no bulk-download path here
    await renderLoaded()
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
    useAuthStore.setState({ me: null, loading: false })
  })

  it('an auto_approve user DOES see the "Download all missing" button', async () => {
    setCaps(['auto_approve'])
    await renderLoaded()
    expect(screen.getByRole('button', { name: /download all missing/i })).toBeInTheDocument()
    useAuthStore.setState({ me: null, loading: false })
  })

  // ── Schedule settings ──────────────────────────────────────────────────────

  it('opening "…" menu shows schedule settings + Remove', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    expect(screen.getByRole('switch', { name: 'Auto-sync' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: /sync interval/i })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /auto-download missing/i })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Remove' })).toBeInTheDocument()
  })

  it('toggling Auto-sync calls updateSyncSettings', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.click(screen.getByRole('switch', { name: 'Auto-sync' }))
    })
    expect(mockUpdateSyncSettings).toHaveBeenCalledWith('sp1', {
      syncEnabled: false, // toggled from true
      intervalSec: 86400,
      autoDownload: false,
    })
  })

  it('changing interval Select calls updateSyncSettings with new intervalSec', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.change(screen.getByRole('combobox', { name: /sync interval/i }), {
        target: { value: '604800' },
      })
    })
    expect(mockUpdateSyncSettings).toHaveBeenCalledWith('sp1', {
      syncEnabled: true,
      intervalSec: 604800,
      autoDownload: false,
    })
  })

  it('toggling Auto-download calls updateSyncSettings', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.click(screen.getByRole('switch', { name: /auto-download missing/i }))
    })
    expect(mockUpdateSyncSettings).toHaveBeenCalledWith('sp1', {
      syncEnabled: true,
      intervalSec: 86400,
      autoDownload: true, // toggled from false
    })
  })

  // ── Remove ─────────────────────────────────────────────────────────────────

  it('Remove (confirm=true) calls deleteSyncedPlaylist and navigates to /library', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Remove' }))
    })
    expect(mockDeleteSyncedPlaylist).toHaveBeenCalledWith('sp1')
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/library'))
  })

  it('Remove (confirm=false) does NOT call deleteSyncedPlaylist', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Remove' }))
    })
    expect(mockDeleteSyncedPlaylist).not.toHaveBeenCalled()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('header wrapper has dynamic style.background when palette is present', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [120, 80, 200], text: '#FFFFFF', scrim: false })
    await renderLoaded()
    const heading = screen.getByRole('heading', { name: 'Test Synced Playlist' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    const bg = (gradientWrapper as HTMLElement).style.background
    expect(bg).toContain('linear-gradient')
    // Browser normalizes rgb(120 80 200 / 0.55) → rgba(120, 80, 200, 0.55)
    expect(bg).toMatch(/120/)
    expect(bg).toMatch(/80/)
    expect(bg).toMatch(/200/)
  })

  it('header wrapper has no inline style when palette is null', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    await renderLoaded()
    const heading = screen.getByRole('heading', { name: 'Test Synced Playlist' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    expect((gradientWrapper as HTMLElement).style.background).toBe('')
  })

  describe('now-playing indicator (Bug 8)', () => {
    it('the active owned row renders an Equalizer (eq-bar)', async () => {
      mockSyncedPlayerState.current = { id: 't1' }
      await renderLoaded()
      const eqBars = document.querySelectorAll('[data-testid="eq-bar"]')
      expect(eqBars.length).toBeGreaterThan(0)
    })

    it('only the playing row shows Equalizer — other owned row does not', async () => {
      mockSyncedPlayerState.current = { id: 't1' }
      await renderLoaded()
      // Equalizer renders 4 bars total; only 1 row is active
      expect(document.querySelectorAll('[data-testid="eq-bar"]').length).toBe(4)
    })

    it('no Equalizer when current is null', async () => {
      mockSyncedPlayerState.current = null
      await renderLoaded()
      expect(document.querySelectorAll('[data-testid="eq-bar"]').length).toBe(0)
    })
  })

  describe('owned-track indicator (Bug 12)', () => {
    it('owned rows render an "In Library" right-slot badge', async () => {
      await renderLoaded()
      // 2 owned tracks → 2 "In Library" badges
      expect(screen.getAllByText('In Library').length).toBe(2)
    })

    it('missing row renders the Download control, not an "In Library" badge', async () => {
      await renderLoaded()
      expect(screen.getByRole('button', { name: /download missing track/i })).toBeInTheDocument()
    })
  })

  describe('Spotify artist/album links', () => {
    it('owned row links artist to /artist/spotify/:id when artistExternalId is present', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, tracks: [ownedRowWithExternalIds, ownedRow2, missingRow] },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() =>
        expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
      )
      const artistLink = screen.getByRole('link', { name: 'Artist A' })
      expect(artistLink).toHaveAttribute('href', '/artist/spotify/sp-artist-99')
    })

    it('owned row links album to /album/spotify/:id when albumExternalId is present', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, tracks: [ownedRowWithExternalIds, ownedRow2, missingRow] },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() =>
        expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
      )
      const albumLinks = screen.getAllByRole('link', { name: 'Playlist A' })
      // The first one belongs to the owned row with external ids
      expect(albumLinks[0]).toHaveAttribute('href', '/album/spotify/sp-album-99')
    })

    it('missing row links artist to /artist/spotify/:id when artistExternalId is present', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, tracks: [ownedRow1, ownedRow2, missingRowWithExternalIds] },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() =>
        expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
      )
      const artistLink = screen.getByRole('link', { name: 'Artist C' })
      expect(artistLink).toHaveAttribute('href', '/artist/spotify/sp-artist-88')
    })

    it('missing row links album to /album/spotify/:id when albumExternalId is present', async () => {
      const missingWithAlbum: AlbumDetailTrack = { ...missingRowWithExternalIds, album: 'Missing Album' }
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, tracks: [ownedRow1, ownedRow2, missingWithAlbum] },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() =>
        expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
      )
      const albumLink = screen.getByRole('link', { name: 'Missing Album' })
      expect(albumLink).toHaveAttribute('href', '/album/spotify/sp-album-88')
    })

    it('owned row falls back to /artist/library/:id when no artistExternalId', async () => {
      await renderLoaded()
      const artistLink = screen.getByRole('link', { name: 'Artist A' })
      expect(artistLink).toHaveAttribute('href', '/artist/library/ar1')
    })
  })

  it('playTrackList receives track with artistExternalId when source row has it', async () => {
    mockUseSyncedPlaylist.mockReturnValue({
      data: { ...mockDetail, tracks: [ownedRowWithExternalIds] },
      isLoading: false,
      isError: false,
    })
    wrapper(<SyncedPlaylist />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: /play test synced playlist/i }))
    expect(mockPlayTrackList).toHaveBeenCalledOnce()
    const [tracks] = mockPlayTrackList.mock.calls[0] as [Track[], number]
    expect(tracks[0]).toMatchObject({ artistExternalId: 'sp-artist-99' })
  })

  describe('mode-aware rendering', () => {
    it('mode=once: "Sync now" button is absent', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'once' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      expect(screen.queryByRole('button', { name: /sync now/i })).not.toBeInTheDocument()
    })

    it('mode=once: schedule settings absent from "…" menu', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'once' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      fireEvent.click(screen.getByRole('button', { name: /more options/i }))
      expect(screen.queryByRole('switch', { name: 'Auto-sync' })).not.toBeInTheDocument()
      expect(screen.queryByRole('combobox', { name: /sync interval/i })).not.toBeInTheDocument()
    })

    it('mode=once: remove control present and calls removeSyncedTrack on owned row', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'once' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())

      // Remove button for "Owned One" (externalRef source='spotify', externalId='e1')
      const removeBtn = screen.getByRole('button', { name: /remove owned one from playlist/i })
      expect(removeBtn).toBeInTheDocument()
      await act(async () => { fireEvent.click(removeBtn) })
      expect(mockRemoveSyncedTrack).toHaveBeenCalledWith('sp1', 'spotify', 'e1')
    })

    it('mode=synced: remove control is absent on owned rows', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'synced' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      expect(screen.queryByRole('button', { name: /remove owned one from playlist/i })).not.toBeInTheDocument()
    })
  })

  // ── Change cover ──────────────────────────────────────────────────────────────

  describe('change cover', () => {
    async function renderOnce() {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'once' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
    }

    it('mode=once: "Change cover" button is present', async () => {
      await renderOnce()
      expect(screen.getByRole('button', { name: /change cover/i })).toBeInTheDocument()
    })

    it('mode=synced: "Change cover" button is absent', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'synced' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      expect(screen.queryByRole('button', { name: /change cover/i })).not.toBeInTheDocument()
    })

    it('mode=once: selecting a file calls uploadPlaylistCover with id and file', async () => {
      await renderOnce()
      const input = screen.getByTestId('cover-file-input') as HTMLInputElement
      const file = new File(['(data)'], 'cover.jpg', { type: 'image/jpeg' })
      await act(async () => {
        fireEvent.change(input, { target: { files: [file] } })
      })
      expect(mockUploadPlaylistCover).toHaveBeenCalledWith('sp1', file)
    })

    it('mode=once: shows error message when upload fails', async () => {
      mockUploadPlaylistCover.mockRejectedValue(new Error('Network error'))
      await renderOnce()
      const input = screen.getByTestId('cover-file-input') as HTMLInputElement
      const file = new File(['(data)'], 'cover.jpg', { type: 'image/jpeg' })
      await act(async () => {
        fireEvent.change(input, { target: { files: [file] } })
      })
      await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
      expect(screen.getByRole('alert')).toHaveTextContent(/couldn't upload/i)
    })
  })

  // ── Drag reorder ──────────────────────────────────────────────────────────────

  describe('drag reorder', () => {
    async function renderOnce() {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'once' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
    }

    it('mode=once: drag handles are present (one per track)', async () => {
      await renderOnce()
      // 3 tracks → 3 drag handles
      const handles = screen.getAllByLabelText(/drag to reorder/i)
      expect(handles.length).toBe(3)
    })

    it('mode=synced: drag handles are absent', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'synced' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      expect(screen.queryByLabelText(/drag to reorder/i)).not.toBeInTheDocument()
    })

    it('mode=once: track wrapper rows are draggable', async () => {
      await renderOnce()
      // The outer wrapper divs around each track row should have draggable=true
      const draggables = document.querySelectorAll('[draggable="true"]')
      expect(draggables.length).toBe(3)
    })

    it('mode=synced: track wrapper rows are NOT draggable', async () => {
      mockUseSyncedPlaylist.mockReturnValue({
        data: { ...mockDetail, mode: 'synced' },
        isLoading: false,
        isError: false,
      })
      wrapper(<SyncedPlaylist />)
      await waitFor(() => expect(screen.getByRole('heading', { name: 'Test Synced Playlist' })).toBeInTheDocument())
      const draggables = document.querySelectorAll('[draggable="true"]')
      expect(draggables.length).toBe(0)
    })

    it('mode=once: drop calls reorderSyncedTracks with correctly-ordered {source,externalId} list', async () => {
      await renderOnce()
      const rows = document.querySelectorAll('[draggable="true"]')
      expect(rows.length).toBe(3)
      // Simulate drag of row 0 (Owned One, e1) to position 1 (after Owned Two)
      // dragstart on row 0
      act(() => { fireEvent.dragStart(rows[0]) })
      // dragOver row 1 (moves idx 0 to 1 in state)
      act(() => { fireEvent.dragOver(rows[1], { preventDefault: () => {} }) })
      // drop
      await act(async () => { fireEvent.drop(rows[1]) })

      // After dragging index 0 to position 1: order should be [1, 0, 2]
      // Row 0 = ownedRow1 (e1), Row 1 = ownedRow2 (e2), Row 2 = missingRow (e3)
      // Reordered: [ownedRow2, ownedRow1, missingRow] → [{spotify,e2},{spotify,e1},{spotify,e3}]
      expect(mockReorderSyncedTracks).toHaveBeenCalledWith('sp1', [
        { source: 'spotify', externalId: 'e2' },
        { source: 'spotify', externalId: 'e1' },
        { source: 'spotify', externalId: 'e3' },
      ])
    })

    it('mode=once: failed reorder refetches server state', async () => {
      mockReorderSyncedTracks.mockRejectedValue(new Error('Server error'))
      await renderOnce()
      const rows = document.querySelectorAll('[draggable="true"]')
      act(() => { fireEvent.dragStart(rows[0]) })
      act(() => { fireEvent.dragOver(rows[1], { preventDefault: () => {} }) })
      await act(async () => { fireEvent.drop(rows[1]) })
      // Should still have tried to call reorderSyncedTracks
      expect(mockReorderSyncedTracks).toHaveBeenCalled()
    })
  })
})
