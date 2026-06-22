import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import SyncedPlaylist from './SyncedPlaylist'
import { makeTrack } from '../test/factories'
import type { SyncedPlaylistDetail, AlbumDetailTrack } from '../lib/types'

// ── Player mock ────────────────────────────────────────────────────────────────
const mockPlayTrackList = vi.fn()

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: { playTrackList: typeof mockPlayTrackList; shuffle: boolean; current: null }) => unknown) =>
    selector({ playTrackList: mockPlayTrackList, shuffle: false, current: null }),
}))

// ── syncedPlaylistApi mocks ────────────────────────────────────────────────────
const mockUseSyncedPlaylist = vi.fn()
const mockSyncNow = vi.fn().mockResolvedValue({})
const mockDownloadMissingForPlaylist = vi.fn().mockResolvedValue([])
const mockUpdateSyncSettings = vi.fn().mockResolvedValue({})
const mockDeleteSyncedPlaylist = vi.fn().mockResolvedValue({})

vi.mock('../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylist: (...args: unknown[]) => mockUseSyncedPlaylist(...args),
  syncNow: (...args: unknown[]) => mockSyncNow(...args),
  downloadMissingForPlaylist: (...args: unknown[]) => mockDownloadMissingForPlaylist(...args),
  updateSyncSettings: (...args: unknown[]) => mockUpdateSyncSettings(...args),
  deleteSyncedPlaylist: (...args: unknown[]) => mockDeleteSyncedPlaylist(...args),
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
  externalRef: { source: 'spotify', externalId: 'e1', title: 'Owned One', artist: 'Artist A', durationMs: 180000 },
  title: 'Owned One',
  artist: 'Artist A',
  trackNumber: 1,
  durationMs: 180000,
}
const ownedRow2: AlbumDetailTrack = {
  state: 'full',
  libraryTrack: track2,
  externalRef: { source: 'spotify', externalId: 'e2', title: 'Owned Two', artist: 'Artist B', durationMs: 200000 },
  title: 'Owned Two',
  artist: 'Artist B',
  trackNumber: 2,
  durationMs: 200000,
}
const missingRow: AlbumDetailTrack = {
  state: 'none',
  libraryTrack: undefined,
  externalRef: { source: 'spotify', externalId: 'e3', title: 'Missing Track', artist: 'Artist C', durationMs: 210000 },
  title: 'Missing Track',
  artist: 'Artist C',
  trackNumber: 3,
  durationMs: 210000,
}

const mockDetail: SyncedPlaylistDetail = {
  id: 'sp1',
  source: 'spotify',
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
  beforeEach(() => {
    vi.clearAllMocks()
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

  it('shows "Synced playlist" eyebrow', async () => {
    await renderLoaded()
    expect(screen.getByText('Synced playlist')).toBeInTheDocument()
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

  it('"Download all missing" button calls downloadMissingForPlaylist with the playlist id', async () => {
    await renderLoaded()
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /download all missing/i }))
    })
    expect(mockDownloadMissingForPlaylist).toHaveBeenCalledWith('sp1')
  })

  it('"Download all missing" button is hidden when nothing is missing', async () => {
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
})
