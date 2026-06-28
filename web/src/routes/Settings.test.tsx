import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
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

describe('Settings Downloaders — two-column layout', () => {
  // spotDL: both track and album; Lidarr: album only
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

  it('renders a Downloaders heading', () => {
    wrap(<Settings />)
    expect(screen.getByRole('heading', { name: /downloaders/i })).toBeInTheDocument()
  })

  it('renders a Song column heading and an Album column heading', () => {
    wrap(<Settings />)
    expect(screen.getAllByText(/song/i).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/album/i).length).toBeGreaterThan(0)
  })

  it('Song column contains only spotdl (has track granularity)', () => {
    wrap(<Settings />)
    const songCol = screen.getByTestId('downloaders-song-col')
    expect(within(songCol).getByText('spotdl')).toBeInTheDocument()
    expect(within(songCol).queryByText('lidarr')).toBeNull()
  })

  it('Album column contains spotdl then lidarr (sorted by album granularity order)', () => {
    wrap(<Settings />)
    const albumCol = screen.getByTestId('downloaders-album-col')
    expect(within(albumCol).getByText('spotdl')).toBeInTheDocument()
    expect(within(albumCol).getByText('lidarr')).toBeInTheDocument()
    // spotdl (album:0) should come before lidarr (album:1)
    const names = within(albumCol).getAllByTestId('downloader-name').map((el) => el.textContent)
    expect(names).toEqual(['spotdl', 'lidarr'])
  })

  it('a downloader with both track and album granularities appears in both columns', () => {
    wrap(<Settings />)
    // spotdl has both track and album
    const songCol = screen.getByTestId('downloaders-song-col')
    const albumCol = screen.getByTestId('downloaders-album-col')
    expect(within(songCol).getByText('spotdl')).toBeInTheDocument()
    expect(within(albumCol).getByText('spotdl')).toBeInTheDocument()
  })

  it('first-row up button is disabled in Song column', () => {
    wrap(<Settings />)
    const songCol = screen.getByTestId('downloaders-song-col')
    const upButtons = within(songCol).getAllByRole('button', { name: /move up/i })
    expect(upButtons[0]).toBeDisabled()
  })

  it('last-row down button is disabled in Album column', () => {
    wrap(<Settings />)
    const albumCol = screen.getByTestId('downloaders-album-col')
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    expect(downButtons[downButtons.length - 1]).toBeDisabled()
  })

  it('clicking down on spotdl in Album column swaps album order — writes config.granularities with swapped album values', async () => {
    wrap(<Settings />)
    const albumCol = screen.getByTestId('downloaders-album-col')
    // spotdl is row 0 in Album column; click its "down" button
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    fireEvent.click(downButtons[0])
    await waitFor(() => expect(mockUpdateAdapter).toHaveBeenCalledTimes(2))

    const calls = mockUpdateAdapter.mock.calls as unknown as Array<[string, { name: string; enabled: boolean; priority: number; config: Record<string, unknown> }]>
    const spotdlCall = calls.find((c) => c[0] === 'dl-1')
    const lidarrCall = calls.find((c) => c[0] === 'dl-2')

    // spotdl should now have album:1 (swapped from lidarr's album:1)
    expect((spotdlCall?.[1].config as Record<string, unknown>)['granularities']).toEqual({ track: 0, album: 1 })
    // lidarr should now have album:0 (swapped from spotdl's album:0)
    expect((lidarrCall?.[1].config as Record<string, unknown>)['granularities']).toEqual({ album: 0 })
  })

  it('reordering Album column does NOT change spotdl track order (Song column independent)', async () => {
    wrap(<Settings />)
    const albumCol = screen.getByTestId('downloaders-album-col')
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    fireEvent.click(downButtons[0])
    await waitFor(() => expect(mockUpdateAdapter).toHaveBeenCalledTimes(2))

    const calls = mockUpdateAdapter.mock.calls as unknown as Array<[string, { config: Record<string, unknown> }]>
    const spotdlCall = calls.find((c) => c[0] === 'dl-1')

    // track order must still be 0 — unchanged
    const granularities = (spotdlCall?.[1].config as Record<string, unknown>)['granularities'] as Record<string, number>
    expect(granularities['track']).toBe(0)
  })
})
