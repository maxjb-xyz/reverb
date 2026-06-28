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

describe('Settings Downloaders section', () => {
  const twoDownloaders: AdapterInstance[] = [
    { id: 'dl-1', type: 'downloader', name: 'spotdl', enabled: true, priority: 1, config: {}, capabilities: [] },
    { id: 'dl-2', type: 'downloader', name: 'lidarr', enabled: true, priority: 2, config: {}, capabilities: ['grain:album'] },
  ]

  beforeEach(() => {
    mockMutate.mockClear()
    mockUpdateAdapter.mockClear()
    mockUseAdapters.mockReturnValue({ data: twoDownloaders })
  })
  afterEach(() => {
    mockUseAdapters.mockReturnValue({ data: [] })
    vi.clearAllMocks()
  })

  it('renders a Downloaders heading', () => {
    wrap(<Settings />)
    expect(screen.getByRole('heading', { name: /downloaders/i })).toBeInTheDocument()
  })

  it('shows each downloader by name', () => {
    wrap(<Settings />)
    expect(screen.getByText('spotdl')).toBeInTheDocument()
    expect(screen.getByText('lidarr')).toBeInTheDocument()
  })

  it('shows granularity label Track for spotdl (no grain:album capability)', () => {
    wrap(<Settings />)
    // spotdl has no grain:album capability → Track
    const spotdlRow = screen.getByText('spotdl').closest('[data-testid]') ??
      screen.getByText('spotdl').parentElement
    expect(spotdlRow).toBeTruthy()
    // Track label should appear in the document
    const trackLabels = screen.getAllByText('Track')
    expect(trackLabels.length).toBeGreaterThan(0)
  })

  it('shows granularity label Album for lidarr (has grain:album capability)', () => {
    wrap(<Settings />)
    const albumLabels = screen.getAllByText('Album')
    expect(albumLabels.length).toBeGreaterThan(0)
  })

  it('the first downloader up button is disabled', () => {
    wrap(<Settings />)
    // First downloader (priority 1) up button should be disabled
    const upButtons = screen.getAllByRole('button', { name: /move up/i })
    expect(upButtons[0]).toBeDisabled()
  })

  it('the last downloader down button is disabled', () => {
    wrap(<Settings />)
    const downButtons = screen.getAllByRole('button', { name: /move down/i })
    expect(downButtons[downButtons.length - 1]).toBeDisabled()
  })

  it('clicking up on the second downloader calls updateAdapter swapping priorities', async () => {
    wrap(<Settings />)
    const upButtons = screen.getAllByRole('button', { name: /move up/i })
    // second row's up button (index 1)
    fireEvent.click(upButtons[1])
    await waitFor(() => expect(mockUpdateAdapter).toHaveBeenCalled())
    // lidarr (priority 2) should get priority 1, and spotdl (priority 1) should get priority 2
    // Cast to any to avoid tuple-type TS noise from vitest mock inference.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const calls = mockUpdateAdapter.mock.calls as unknown as Array<[string, { priority: number }]>
    // Two adapter updates: lidarr → priority 1, spotdl → priority 2
    expect(calls).toHaveLength(2)
    const lidarrCall = calls.find((c) => c[0] === 'dl-2')
    const spotdlCall = calls.find((c) => c[0] === 'dl-1')
    expect(lidarrCall?.[1].priority).toBe(1)
    expect(spotdlCall?.[1].priority).toBe(2)
  })
})
