import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TopBar } from './TopBar'
import { useUI } from '../../lib/uiStore'
import { useDownloads } from '../../lib/downloadStore'
import type { DownloadJob } from '../../lib/types'

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
  })

  it('renders all required accessible controls', () => {
    renderBar()
    expect(screen.getByRole('button', { name: /back/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /forward/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /home/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /search/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /downloads/i })).toBeInTheDocument()
  })

  it('home button navigates to /', () => {
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /home/i }))
    expect(mockNavigate).toHaveBeenCalledWith('/')
  })

  it('search button navigates to /search', () => {
    renderBar()
    fireEvent.click(screen.getByRole('button', { name: /search/i }))
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
