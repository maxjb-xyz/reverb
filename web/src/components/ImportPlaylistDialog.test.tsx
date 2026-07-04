import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ImportPlaylistDialog } from './ImportPlaylistDialog'
import type { SyncedPlaylistDetail } from '../lib/types'

// ── Mocks ─────────────────────────────────────────────────────────────────────

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../lib/syncedPlaylistApi', () => ({
  importPlaylist: vi.fn(),
}))

vi.mock('../lib/libraryApi', () => ({
  importPlaylistOnce: vi.fn(),
}))

// Default: user with no capabilities. Tests can override via setAuth().
vi.mock('../lib/authStore', () => ({

  useAuthStore: vi.fn((selector: (s: any) => unknown) => selector({ can: () => false })),
}))

import { importPlaylist } from '../lib/syncedPlaylistApi'
import { importPlaylistOnce } from '../lib/libraryApi'
import { useAuthStore } from '../lib/authStore'

// ── Helpers ───────────────────────────────────────────────────────────────────


function makeDetail(overrides: Partial<SyncedPlaylistDetail> = {}): SyncedPlaylistDetail {
  return {
    id: 'sp-abc123',
    source: 'spotify',
    externalId: 'abc123',
    name: 'My Spotify Playlist',
    syncEnabled: true,
    syncIntervalSec: 3600,
    autoDownload: false,
    lastSyncedAt: 0,
    trackCount: 10,
    ownedCount: 8,
    totalCount: 10,
    tracks: [],
    ...overrides,
  }
}

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Routes>
          <Route path="/" element={<>{ui}</>} />
          <Route path="/playlist/:id" element={<div data-testid="synced-playlist-page" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('ImportPlaylistDialog', () => {
  function setAuth(caps: string[]) {
    vi.mocked(useAuthStore).mockImplementation((selector: (s: any) => unknown) =>
      selector({ can: (cap: string) => caps.includes(cap) }),
    )
  }

  beforeEach(() => {
    vi.mocked(importPlaylist).mockReset()
    vi.mocked(importPlaylistOnce).mockReset()
    mockNavigate.mockReset()
    // Reset to no-caps default

    vi.mocked(useAuthStore).mockImplementation((selector: (s: any) => unknown) =>
      selector({ can: () => false }),
    )
  })
  afterEach(() => vi.clearAllMocks())

  it('renders nothing when open=false', () => {
    render(wrap(<ImportPlaylistDialog open={false} onClose={vi.fn()} />))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders dialog heading when open=true', () => {
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText(/import from spotify/i)).toBeInTheDocument()
  })

  it('renders URL input, toggle, Import and Cancel buttons (auto_approve user)', () => {
    setAuth(['auto_approve'])
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    expect(screen.getByLabelText(/playlist url/i)).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /download missing now/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^import$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
  })

  it('Import button is disabled when URL is empty', () => {
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    expect(screen.getByRole('button', { name: /^import$/i })).toBeDisabled()
  })

  it('Import button is enabled after typing a URL', () => {
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/abc' },
    })
    expect(screen.getByRole('button', { name: /^import$/i })).not.toBeDisabled()
  })

  it('calls importPlaylist(url, downloadMissing) and navigates to /playlist/:id on success', async () => {
    setAuth(['auto_approve'])
    const detail = makeDetail({ id: 'sp-xyz' })
    vi.mocked(importPlaylist).mockResolvedValue(detail)
    const onClose = vi.fn()

    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/xyz' },
    })

    // Toggle "Download missing now" (only visible for auto_approve users)
    fireEvent.click(screen.getByRole('switch', { name: /download missing now/i }))

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    await waitFor(() => {
      expect(importPlaylist).toHaveBeenCalledWith(
        'https://open.spotify.com/playlist/xyz',
        true,
      )
    })

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/playlist/sp-xyz')
    })

    expect(onClose).toHaveBeenCalled()
  })

  it('calls importPlaylist with downloadMissing=false by default', async () => {
    const detail = makeDetail({ id: 'sp-def' })
    vi.mocked(importPlaylist).mockResolvedValue(detail)
    const onClose = vi.fn()

    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/def' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    await waitFor(() => {
      expect(importPlaylist).toHaveBeenCalledWith(
        'https://open.spotify.com/playlist/def',
        false,
      )
    })
  })

  it('renders inline error and does NOT navigate when importPlaylist rejects', async () => {
    vi.mocked(importPlaylist).mockRejectedValue(new Error('Playlist not found or private'))
    const onClose = vi.fn()

    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/private' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/playlist not found or private/i)
    expect(mockNavigate).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('shows busy state while request is in-flight and disables Import', async () => {
    let resolve!: (v: SyncedPlaylistDetail) => void
    vi.mocked(importPlaylist).mockReturnValue(new Promise<SyncedPlaylistDetail>((r) => { resolve = r }))

    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/slow' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /importing/i })).toBeDisabled()
    })

    // Resolve to avoid hanging
    resolve(makeDetail())
  })

  it('Cancel is clickable while import is in-flight: calls onClose and does NOT navigate', async () => {
    let resolve!: (v: SyncedPlaylistDetail) => void
    vi.mocked(importPlaylist).mockReturnValue(new Promise<SyncedPlaylistDetail>((r) => { resolve = r }))
    const onClose = vi.fn()

    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/inflight' },
    })

    // Kick off the import (leaves busy=true, promise unresolved)
    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    // Wait until busy state is active
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /importing/i })).toBeDisabled()
    })

    // Cancel must still be enabled and callable
    const cancelBtn = screen.getByRole('button', { name: /cancel/i })
    expect(cancelBtn).not.toBeDisabled()
    fireEvent.click(cancelBtn)

    expect(onClose).toHaveBeenCalled()
    expect(mockNavigate).not.toHaveBeenCalled()

    // Resolve to avoid hanging promise
    resolve(makeDetail())
  })

  it('calls onClose when Cancel is clicked', () => {
    const onClose = vi.fn()
    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(onClose).toHaveBeenCalled()
  })

  it('calls onClose when backdrop is clicked', () => {
    const onClose = vi.fn()
    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))
    fireEvent.click(screen.getByTestId('import-playlist-backdrop'))
    expect(onClose).toHaveBeenCalled()
  })

  it('renders both Sync and One-time tabs (default Sync)', () => {
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    const syncTab = screen.getByRole('tab', { name: /sync/i })
    const oneTimeTab = screen.getByRole('tab', { name: /one-time/i })
    expect(syncTab).toBeInTheDocument()
    expect(oneTimeTab).toBeInTheDocument()
    expect(syncTab).toHaveAttribute('aria-selected', 'true')
    expect(oneTimeTab).toHaveAttribute('aria-selected', 'false')
  })

  it('selecting One-time + submitting calls importPlaylistOnce and navigates to /playlist/:id', async () => {
    vi.mocked(importPlaylistOnce).mockResolvedValue(makeDetail({ id: 'pl-snap', mode: 'once' }))
    const onClose = vi.fn()

    render(wrap(<ImportPlaylistDialog open onClose={onClose} />))

    fireEvent.click(screen.getByRole('tab', { name: /one-time/i }))

    fireEvent.change(screen.getByLabelText(/playlist url/i), {
      target: { value: 'https://open.spotify.com/playlist/THEID' },
    })

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }))

    await waitFor(() => {
      expect(importPlaylistOnce).toHaveBeenCalledWith('https://open.spotify.com/playlist/THEID')
    })

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/playlist/pl-snap')
    })

    expect(onClose).toHaveBeenCalled()
  })

  it('One-time mode hides the Download missing toggle', () => {
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    fireEvent.click(screen.getByRole('tab', { name: /one-time/i }))
    expect(screen.queryByRole('switch', { name: /download missing now/i })).not.toBeInTheDocument()
  })

  // ── "Download missing now" capability gating (Fix 2) ─────────────────────

  it('request-only user (no auto_approve) does NOT see the "Download missing now" toggle in sync mode', () => {
    setAuth(['request', 'create_playlists']) // can import, cannot auto_approve
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    // Sync mode is the default — toggle must be absent for non-auto_approve user
    expect(screen.queryByRole('switch', { name: /download missing now/i })).not.toBeInTheDocument()
  })

  it('auto_approve user sees the "Download missing now" toggle in sync mode', () => {
    setAuth(['auto_approve', 'create_playlists'])
    render(wrap(<ImportPlaylistDialog open onClose={vi.fn()} />))
    expect(screen.getByRole('switch', { name: /download missing now/i })).toBeInTheDocument()
  })
})
