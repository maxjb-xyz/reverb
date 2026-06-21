import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Admin from './Admin'

// ── Mock adaptersApi ──────────────────────────────────────────────────────────
const mockUseAdapters = vi.fn()
const mockUseAvailableAdapters = vi.fn()
const mockUsePendingRestart = vi.fn()
const mockCreateAdapter = vi.fn()
const mockUpdateAdapter = vi.fn()
const mockDeleteAdapter = vi.fn()

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => mockUseAdapters(),
  useAvailableAdapters: () => mockUseAvailableAdapters(),
  usePendingRestart: () => mockUsePendingRestart(),
  createAdapter: (...args: unknown[]) => mockCreateAdapter(...args),
  updateAdapter: (...args: unknown[]) => mockUpdateAdapter(...args),
  deleteAdapter: (...args: unknown[]) => mockDeleteAdapter(...args),
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))

// ── Default mock return values ────────────────────────────────────────────────
const makeAdapter = (overrides = {}) => ({
  id: 'inst-1',
  type: 'library',
  name: 'LocalFS',
  enabled: true,
  priority: 0,
  config: {},
  ...overrides,
})

const makeAvailable = (overrides = {}) => ({
  type: 'library',
  name: 'LocalFS',
  configSchema: { fields: [{ key: 'path', label: 'Path', type: 'text', required: true, secret: false }] },
  capabilities: [],
  ...overrides,
})

function setupDefaultMocks() {
  mockUseAdapters.mockReturnValue({ data: [], isLoading: false })
  mockUseAvailableAdapters.mockReturnValue({ data: [], isLoading: false })
  mockUsePendingRestart.mockReturnValue({ data: { pendingRestart: false } })
}

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

describe('Admin', () => {
  beforeEach(() => {
    setupDefaultMocks()
    mockCreateAdapter.mockResolvedValue({ data: makeAdapter(), pendingRestart: false })
    mockUpdateAdapter.mockResolvedValue({ data: makeAdapter(), pendingRestart: false })
    mockDeleteAdapter.mockResolvedValue({ ok: true, pendingRestart: false })
  })

  afterEach(() => vi.clearAllMocks())

  // ── Header + tabs ────────────────────────────────────────────────────────────
  it('renders the Admin heading', () => {
    wrap(<Admin />)
    expect(screen.getByRole('heading', { name: /admin/i })).toBeInTheDocument()
  })

  it('shows Providers, Server and Users tabs', () => {
    wrap(<Admin />)
    expect(screen.getByRole('button', { name: /providers/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /server/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /users/i })).toBeInTheDocument()
  })

  // ── Providers tab — three section headings ───────────────────────────────────
  it('Providers tab renders all three section headings', () => {
    wrap(<Admin />)
    // Use heading role to avoid matching EmptyState text that also contains these words
    expect(screen.getByRole('heading', { name: /library providers/i })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /search providers/i })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /downloaders/i })).toBeInTheDocument()
  })

  it('shows skeleton loaders while adapters are loading', () => {
    mockUseAdapters.mockReturnValue({ data: undefined, isLoading: true })
    mockUseAvailableAdapters.mockReturnValue({ data: undefined, isLoading: true })
    wrap(<Admin />)
    expect(screen.getByLabelText(/loading providers/i)).toBeInTheDocument()
  })

  // ── Add library opens AdapterForm ────────────────────────────────────────────
  it('clicking "Add library" opens the AdapterForm dialog', () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'library', name: 'LocalFS' })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add library/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    // AdapterForm renders the field from the configSchema
    expect(screen.getByLabelText(/path/i)).toBeInTheDocument()
  })

  it('submitting the add form calls createAdapter', async () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'library', name: 'LocalFS' })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add library/i }))
    // Fill in the path field
    fireEvent.change(screen.getByLabelText(/path/i), { target: { value: '/music' } })
    // Submit
    fireEvent.click(screen.getByRole('button', { name: /^add$/i }))
    await waitFor(() =>
      expect(mockCreateAdapter).toHaveBeenCalledWith({
        type: 'library',
        name: 'LocalFS',
        enabled: true,
        priority: 0,
        config: { path: '/music' },
      })
    )
  })

  // ── RestartBanner ────────────────────────────────────────────────────────────
  it('shows RestartBanner when usePendingRestart returns true', () => {
    mockUsePendingRestart.mockReturnValue({ data: { pendingRestart: true } })
    wrap(<Admin />)
    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(screen.getByText(/restart reverb/i)).toBeInTheDocument()
  })

  it('does not show RestartBanner when pendingRestart is false', () => {
    mockUsePendingRestart.mockReturnValue({ data: { pendingRestart: false } })
    wrap(<Admin />)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  // ── Server tab ───────────────────────────────────────────────────────────────
  it('Server tab shows server info content', () => {
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /server/i }))
    expect(screen.getByRole('heading', { name: /server info/i })).toBeInTheDocument()
    expect(screen.getByText(/applying config changes/i)).toBeInTheDocument()
  })

  // ── Users tab — honest EmptyState ────────────────────────────────────────────
  it('Users tab shows the honest EmptyState (no fake table)', () => {
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /users/i }))
    expect(screen.getByText(/single-admin for now/i)).toBeInTheDocument()
    expect(screen.getByText(/multi-user/i)).toBeInTheDocument()
    // No table or fake user rows
    expect(screen.queryByRole('table')).toBeNull()
  })

  // ── Adapter instances render ─────────────────────────────────────────────────
  it('renders adapter instances in the correct section', () => {
    mockUseAdapters.mockReturnValue({
      data: [
        makeAdapter({ id: '1', type: 'library', name: 'LocalFS' }),
        makeAdapter({ id: '2', type: 'search', name: 'MusicBrainz', priority: 1 }),
        makeAdapter({ id: '3', type: 'downloader', name: 'spotDL', priority: 1 }),
      ],
      isLoading: false,
    })
    wrap(<Admin />)
    expect(screen.getByText('LocalFS')).toBeInTheDocument()
    expect(screen.getByText('MusicBrainz')).toBeInTheDocument()
    expect(screen.getByText('spotDL')).toBeInTheDocument()
  })

  // ── Cancel closes form ────────────────────────────────────────────────────────
  it('Cancel button closes the AdapterForm', () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'library', name: 'LocalFS' })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add library/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
