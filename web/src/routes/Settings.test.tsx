import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Settings from './Settings'
import type { AdapterInstance } from '../lib/adaptersApi'

const mockMutate = vi.fn()
// Defined at module scope so vi.mock factory can close over it.
const mockUpdateAdapter = vi.fn(() => Promise.resolve({ data: {}, pendingRestart: false }))
const mockUseAdapters = vi.fn(() => ({ data: [] as AdapterInstance[] }))

vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({ data: { accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' } })),
  useUpdateSettings: vi.fn(() => ({ mutate: mockMutate })),
  putSettings: vi.fn(() => Promise.resolve({ accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' })),
  applyAccent: vi.fn(),
}))

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => mockUseAdapters(),
  updateAdapter: (...args: Parameters<typeof mockUpdateAdapter>) => mockUpdateAdapter(...args),
}))


function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('Settings', () => {
  beforeEach(() => {
    mockMutate.mockClear()
  })
  afterEach(() => vi.clearAllMocks())

  it('renders the Settings header', () => {
    wrap(<Settings />)
    expect(screen.getByRole('heading', { name: /settings/i })).toBeInTheDocument()
  })

  it('shows Appearance tab only', () => {
    wrap(<Settings />)
    expect(screen.getByRole('button', { name: /appearance/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^account$/i })).toBeNull()
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
  it('does NOT render a "Default downloader" control (removed in downloader-chains)', () => {
    wrap(<Settings />)
    expect(screen.queryByLabelText('Default downloader')).not.toBeInTheDocument()
    expect(screen.queryByText(/default downloader/i)).not.toBeInTheDocument()
  })
})

describe('Settings Downloaders — REMOVED (moved to Admin)', () => {
  // spotDL: both track and album; Lidarr: album only — adapters present but
  // the Downloaders section must NOT render in Settings any more.
  const mockAdapters: AdapterInstance[] = [
    {
      id: 'dl-1',
      type: 'downloader',
      name: 'spotdl',
      enabled: true,
      priority: 0,
      config: {},
      capabilities: [],
      granularities: { track: 0, album: 0 },
      supportedGranularities: ['track', 'album'],
    },
    {
      id: 'dl-2',
      type: 'downloader',
      name: 'lidarr',
      enabled: true,
      priority: 1,
      config: {},
      capabilities: [],
      granularities: { album: 1 },
      supportedGranularities: ['album'],
    },
  ]

  beforeEach(() => {
    mockMutate.mockClear()
    mockUpdateAdapter.mockClear()
    mockUseAdapters.mockReturnValue({ data: mockAdapters })
  })
  afterEach(() => {
    mockUseAdapters.mockReturnValue({ data: [] })
    vi.clearAllMocks()
  })

  it('does NOT render a Downloaders heading in Settings', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('heading', { name: /downloaders/i })).not.toBeInTheDocument()
  })

  it('does NOT render the Song column (downloaders-song-col) in Settings', () => {
    wrap(<Settings />)
    expect(screen.queryByTestId('downloaders-song-col')).not.toBeInTheDocument()
  })

  it('does NOT render the Album column (downloaders-album-col) in Settings', () => {
    wrap(<Settings />)
    expect(screen.queryByTestId('downloaders-album-col')).not.toBeInTheDocument()
  })
})
