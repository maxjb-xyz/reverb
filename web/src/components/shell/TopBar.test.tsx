import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TopBar } from './TopBar'
import { useUI } from '../../lib/uiStore'
import { useDownloads } from '../../lib/downloadStore'
import { useSearch } from '../../lib/searchStore'
import { useAuthStore } from '../../lib/authStore'
import type { DownloadJob } from '../../lib/types'

function setMe(capabilities: string[]) {
  useAuthStore.setState({
    me: {
      id: 'u1', username: 'u', roleId: 'r', roleName: 'R', isOwner: false, capabilities,
    },
    loading: false,
  })
}

// Mock useNavigate so we can assert navigation
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

// Stub fetch globally — TopBar's logout calls fetch
global.fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => '' })

function job(id: string, status: 'queued' | 'running' | 'completed'): DownloadJob {
  return {
    id, dedupKey: id, status, progress: 0,
    downloaderName: '', priority: 0, attempts: 0,
    source: 'spotify', externalId: id, playWhenReady: false,
    createdAt: Date.now() / 1000, startedAt: 0, finishedAt: 0,
  }
}

function renderBar() {
  return render(
    <MemoryRouter>
      <TopBar />
    </MemoryRouter>,
  )
}

describe('TopBar', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    vi.mocked(global.fetch).mockClear()
    useUI.setState({ rightPanel: null })
    useDownloads.setState({ jobs: {} })
    useSearch.setState({ query: '', mode: 'library' })
    useAuthStore.setState({ me: null, loading: false })
  })

  it('renders all required accessible controls', () => {
    renderBar()
    expect(screen.getByRole('button', { name: /back/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /forward/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /home/i })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: /search/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /downloads/i })).toBeInTheDocument()
  })

  it('home button navigates to /', () => {
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /home/i }))
    expect(mockNavigate).toHaveBeenCalledWith('/')
  })

  it('typing in the search input updates the shared query but does NOT navigate', () => {
    renderBar()
    const input = screen.getByRole('textbox', { name: /search/i })
    fireEvent.change(input, { target: { value: 'daft punk' } })
    // Typing is now a live typeahead — it must NOT route to /search.
    expect(mockNavigate).not.toHaveBeenCalled()
    expect(useSearch.getState().query).toBe('daft punk')
  })

  it('submitting the search form (Enter) navigates to /search', () => {
    renderBar()
    const input = screen.getByRole('textbox', { name: /search/i })
    fireEvent.change(input, { target: { value: 'daft punk' } })
    // Submitting the enclosing form (Enter) is what navigates now.
    fireEvent.submit(input)
    expect(mockNavigate).toHaveBeenCalledWith('/search')
  })

  it('downloads button calls togglePanel("downloads")', () => {
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /downloads/i }))
    expect(useUI.getState().rightPanel).toBe('downloads')
  })

  it('shows no badge when there are no active downloads', () => {
    renderBar()
    expect(screen.queryByTestId('downloads-badge')).not.toBeInTheDocument()
  })

  it('shows badge with count when there are active downloads', () => {
    useDownloads.setState({ jobs: { a: job('a', 'queued'), b: job('b', 'running'), c: job('c', 'completed') } })
    renderBar()
    const badge = screen.getByTestId('downloads-badge')
    expect(badge).toBeInTheDocument()
    // 'a' and 'b' are active; 'c' is done
    expect(badge.textContent).toBe('2')
  })

  it('avatar button opens a menu with Logout', () => {
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.getByRole('menuitem', { name: /logout/i })).toBeInTheDocument()
  })

  it('Account and Settings menu items are available to all authenticated users', () => {
    setMe([]) // no manager caps
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.getByRole('menuitem', { name: /account/i })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: /settings/i })).toBeInTheDocument()
  })

  it('Admin menu item is hidden when the user lacks all manager capabilities', () => {
    setMe(['auto_approve', 'request', 'can_create_playlists'])
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.queryByRole('menuitem', { name: /admin/i })).not.toBeInTheDocument()
  })

  it('Admin menu item is shown when the user is an admin', () => {
    setMe(['is_admin'])
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.getByRole('menuitem', { name: /admin/i })).toBeInTheDocument()
  })

  it('Admin menu item is shown when the user can manage the library', () => {
    setMe(['can_manage_library'])
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.getByRole('menuitem', { name: /admin/i })).toBeInTheDocument()
  })

  it('Admin menu item is shown when the user can manage users', () => {
    setMe(['can_manage_users'])
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    expect(screen.getByRole('menuitem', { name: /admin/i })).toBeInTheDocument()
  })

  it('logout POSTs to /api/v1/auth/logout', async () => {
    // jsdom does not allow spying on window.location.reload; stub it via defineProperty
    Object.defineProperty(window, 'location', {
      value: { ...window.location, reload: vi.fn() },
      writable: true,
    })
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /account|user|avatar|menu/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /logout/i }))
    // allow promise to flush
    await vi.waitFor(() => {
      expect(global.fetch).toHaveBeenCalledWith(
        '/api/v1/auth/logout',
        expect.objectContaining({ method: 'POST' }),
      )
    })
  })
})
