import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Settings from './Settings'

const mockMutate = vi.fn()

vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({ data: { accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' } })),
  useUpdateSettings: vi.fn(() => ({ mutate: mockMutate })),
  putSettings: vi.fn(() => Promise.resolve({ accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' })),
  applyAccent: vi.fn(),
}))

// AccentSwatches is rendered inside Settings — we need settingsApi mocked above and
// the component itself to render (not stub it out), so we only stub the api module
// to prevent real fetch calls from the logout button.
vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => ({
    data: [
      { id: 'a1', type: 'downloader', name: 'spotdl', enabled: true, priority: 1, config: {} },
      { id: 'a2', type: 'downloader', name: 'lidarr', enabled: true, priority: 2, config: {} },
    ],
  }),
}))

vi.mock('../lib/api', () => ({
  api: {
    get: vi.fn(() => Promise.resolve({})),
    post: vi.fn(() => Promise.resolve({})),
    put: vi.fn(() => Promise.resolve({})),
    del: vi.fn(() => Promise.resolve({})),
  },
  ApiError: class ApiError extends Error {
    status: number
    constructor(method: string, path: string, status: number) {
      super(`${method} ${path} -> ${status}`)
      this.name = 'ApiError'
      this.status = status
    }
  },
}))

import { api } from '../lib/api'

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('Settings', () => {
  beforeEach(() => {
    mockMutate.mockClear()
    // Stub window.location.reload so tests don't actually reload
    Object.defineProperty(window, 'location', {
      value: { ...window.location, reload: vi.fn() },
      writable: true,
    })
  })
  afterEach(() => vi.clearAllMocks())

  it('renders the Settings header', () => {
    wrap(<Settings />)
    expect(screen.getByRole('heading', { name: /settings/i })).toBeInTheDocument()
  })

  it('shows Appearance and Account tabs', () => {
    wrap(<Settings />)
    expect(screen.getByRole('button', { name: /appearance/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /account/i })).toBeInTheDocument()
  })

  it('shows the accent swatches on the Appearance tab', () => {
    wrap(<Settings />)
    // Red is the default preset swatch
    expect(screen.getByRole('button', { name: /red \(default\)/i })).toBeInTheDocument()
  })

  it('shows the dynamic background toggle on the Appearance tab', () => {
    wrap(<Settings />)
    expect(screen.getByRole('switch', { name: /dynamic album background/i })).toBeInTheDocument()
  })

  it('toggling dynamic background calls useUpdateSettings mutate', async () => {
    wrap(<Settings />)
    const toggle = screen.getByRole('switch', { name: /dynamic album background/i })
    fireEvent.click(toggle)
    await waitFor(() =>
      expect(mockMutate).toHaveBeenCalledWith({ dynamicBackground: false })
    )
  })

  it('shows the Account tab content on click', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /account/i }))
    expect(screen.getByRole('button', { name: /log out/i })).toBeInTheDocument()
  })

  it('Log out calls POST /auth/logout', async () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /account/i }))
    fireEvent.click(screen.getByRole('button', { name: /log out/i }))
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/auth/logout'))
  })

  it('NO adapter UI present — no Add library button', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('button', { name: /add library/i })).toBeNull()
  })

  it('NO adapter UI present — no Test button', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('button', { name: /^test$/i })).toBeNull()
  })

  it('NO adapter UI present — no Remove button', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('button', { name: /remove/i })).toBeNull()
  })
})

describe('Settings default downloader', () => {
  beforeEach(() => mockMutate.mockClear())
  it('shows a Default downloader select and saves the choice', () => {
    wrap(<Settings />)
    const select = screen.getByLabelText('Default downloader')
    fireEvent.change(select, { target: { value: 'lidarr' } })
    expect(mockMutate).toHaveBeenCalledWith({ defaultDownloader: 'lidarr' })
  })
})
