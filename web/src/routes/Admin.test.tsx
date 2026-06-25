import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Admin from './Admin'

// ── Mock adaptersApi ──────────────────────────────────────────────────────────
const mockUseAdapters = vi.fn()
const mockUseAvailableAdapters = vi.fn()
const mockCreateAdapter = vi.fn()
const mockUpdateAdapter = vi.fn()
const mockDeleteAdapter = vi.fn()

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => mockUseAdapters(),
  useAvailableAdapters: () => mockUseAvailableAdapters(),
  createAdapter: (...args: unknown[]) => mockCreateAdapter(...args),
  updateAdapter: (...args: unknown[]) => mockUpdateAdapter(...args),
  deleteAdapter: (...args: unknown[]) => mockDeleteAdapter(...args),
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))

// ── Mock settingsApi (Library backend switch) ───────────────────────────────────
const mockUseSettings = vi.fn()
const mockUpdateSettingsMutate = vi.fn()
vi.mock('../lib/settingsApi', () => ({
  useSettings: () => mockUseSettings(),
  useUpdateSettings: () => ({ mutate: mockUpdateSettingsMutate }),
}))

const settingsData = (libraryBackendMode: string) => ({
  data: { accentColor: '#F0354B', dynamicBackground: true, defaultDownloader: '', libraryBackendMode },
})

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

// A library provider available for the External form (subsonic-shaped schema).
const subsonicAvailable = makeAvailable({
  type: 'library',
  name: 'subsonic',
  configSchema: {
    fields: [
      { key: 'url', label: 'Server URL', type: 'string', required: true, secret: false },
      { key: 'username', label: 'Username', type: 'string', required: true, secret: false },
      { key: 'password', label: 'Password', type: 'string', required: true, secret: true },
    ],
  },
})

function setupDefaultMocks() {
  mockUseAdapters.mockReturnValue({ data: [], isLoading: false })
  mockUseAvailableAdapters.mockReturnValue({ data: [], isLoading: false })
  mockUseSettings.mockReturnValue(settingsData('built-in'))
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

  // ── Library backend: single-active switch (lives in Providers, not user Settings) ─
  it('Providers tab shows the Library backend switch and saves the choice', () => {
    wrap(<Admin />)
    const select = screen.getByLabelText('Library backend')
    fireEvent.change(select, { target: { value: 'external' } })
    expect(mockUpdateSettingsMutate).toHaveBeenCalledWith({ libraryBackendMode: 'external' })
  })

  it('Built-in mode shows the bundled hint and NOT the external server form', () => {
    mockUseSettings.mockReturnValue(settingsData('built-in'))
    mockUseAvailableAdapters.mockReturnValue({ data: [subsonicAvailable], isLoading: false })
    wrap(<Admin />)
    expect(screen.getByText(/bundled music server/i)).toBeInTheDocument()
    expect(screen.queryByLabelText('Server URL')).toBeNull()
  })

  it('External mode shows the single Subsonic server form (no "add" list)', () => {
    mockUseSettings.mockReturnValue(settingsData('external'))
    mockUseAvailableAdapters.mockReturnValue({ data: [subsonicAvailable], isLoading: false })
    wrap(<Admin />)
    expect(screen.getByLabelText('Server URL')).toBeInTheDocument()
    // It's a switch, not a list: no "Add library" affordance anywhere.
    expect(screen.queryByRole('button', { name: /add library/i })).toBeNull()
  })

  it('External mode with no instance: saving the form creates the library adapter', async () => {
    mockUseSettings.mockReturnValue(settingsData('external'))
    mockUseAvailableAdapters.mockReturnValue({ data: [subsonicAvailable], isLoading: false })
    mockUseAdapters.mockReturnValue({ data: [], isLoading: false })
    wrap(<Admin />)
    fireEvent.change(screen.getByLabelText('Server URL'), { target: { value: 'http://nav:4533' } })
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'pw' } })
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))
    await waitFor(() =>
      expect(mockCreateAdapter).toHaveBeenCalledWith({
        type: 'library',
        name: 'subsonic',
        enabled: true,
        priority: 0,
        config: { url: 'http://nav:4533', username: 'admin', password: 'pw' },
      })
    )
  })

  // ── Unset setting must resolve to the EFFECTIVE mode (regression: "" showed
  //    "Built-in" in the dropdown while rendering the External form) ────────────
  it('Unset setting + a configured library resolves to External (form + dropdown agree)', () => {
    mockUseSettings.mockReturnValue(settingsData('')) // never explicitly chosen
    mockUseAvailableAdapters.mockReturnValue({ data: [subsonicAvailable], isLoading: false })
    mockUseAdapters.mockReturnValue({
      data: [makeAdapter({ id: '1', type: 'library', name: 'subsonic', enabled: true })],
      isLoading: false,
    })
    wrap(<Admin />)
    const select = screen.getByLabelText('Library backend') as HTMLSelectElement
    expect(select.value).toBe('external')
    expect(screen.getByLabelText('Server URL')).toBeInTheDocument()
  })

  it('Unset setting + no library configured resolves to Built-in (hint, no form)', () => {
    mockUseSettings.mockReturnValue(settingsData(''))
    mockUseAvailableAdapters.mockReturnValue({ data: [subsonicAvailable], isLoading: false })
    mockUseAdapters.mockReturnValue({ data: [], isLoading: false })
    wrap(<Admin />)
    const select = screen.getByLabelText('Library backend') as HTMLSelectElement
    expect(select.value).toBe('built-in')
    expect(screen.queryByLabelText('Server URL')).toBeNull()
  })

  // ── Providers tab — section headings (library is a switch card, not a list) ───
  it('Providers tab renders the library switch + search/downloader sections', () => {
    wrap(<Admin />)
    expect(screen.getByRole('heading', { name: /library backend/i })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /search providers/i })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /downloaders/i })).toBeInTheDocument()
    // No multi-instance "Library providers" list anymore.
    expect(screen.queryByRole('heading', { name: /library providers/i })).toBeNull()
  })

  it('shows skeleton loaders while adapters are loading', () => {
    mockUseAdapters.mockReturnValue({ data: undefined, isLoading: true })
    mockUseAvailableAdapters.mockReturnValue({ data: undefined, isLoading: true })
    wrap(<Admin />)
    expect(screen.getByLabelText(/loading providers/i)).toBeInTheDocument()
  })

  // ── Add (search section — still a multi-instance AdapterSection) ──────────────
  it('clicking "Add search" opens the inline AdapterForm (single provider auto-selected)', () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'search', name: 'MusicBrainz', configSchema: { fields: [{ key: 'path', label: 'Path', type: 'text', required: true, secret: false }] } })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    expect(screen.getByText('Add MusicBrainz')).toBeInTheDocument()
    expect(screen.getByLabelText(/path/i)).toBeInTheDocument()
  })

  it('submitting the add form calls createAdapter', async () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'search', name: 'MusicBrainz', configSchema: { fields: [{ key: 'path', label: 'Path', type: 'text', required: true, secret: false }] } })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    fireEvent.change(screen.getByLabelText(/path/i), { target: { value: '/x' } })
    fireEvent.click(screen.getByRole('button', { name: /^add$/i }))
    await waitFor(() =>
      expect(mockCreateAdapter).toHaveBeenCalledWith({
        type: 'search',
        name: 'MusicBrainz',
        enabled: true,
        priority: 0,
        config: { path: '/x' },
      })
    )
  })

  // ── Live apply (no restart banner) ───────────────────────────────────────────
  it('never shows a restart-to-apply banner (changes apply live)', () => {
    wrap(<Admin />)
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText(/restart reverb/i)).toBeNull()
  })

  // ── Server tab ───────────────────────────────────────────────────────────────
  it('Server tab shows server info with live-apply copy', () => {
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /server/i }))
    expect(screen.getByRole('heading', { name: /server info/i })).toBeInTheDocument()
    expect(screen.getByText(/applying config changes/i)).toBeInTheDocument()
    expect(screen.getByText(/no restart required/i)).toBeInTheDocument()
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

  // ── Adapter instances render (search/downloader are multi-instance lists) ─────
  it('renders search and downloader instances in their sections', () => {
    mockUseAdapters.mockReturnValue({
      data: [
        makeAdapter({ id: '2', type: 'search', name: 'MusicBrainz', priority: 1 }),
        makeAdapter({ id: '3', type: 'downloader', name: 'spotDL', priority: 1 }),
      ],
      isLoading: false,
    })
    wrap(<Admin />)
    expect(screen.getByText('MusicBrainz')).toBeInTheDocument()
    expect(screen.getByText('spotDL')).toBeInTheDocument()
  })

  // ── Cancel closes form (search section) ──────────────────────────────────────
  it('Cancel button closes the inline AdapterForm', () => {
    mockUseAvailableAdapters.mockReturnValue({
      data: [makeAvailable({ type: 'search', name: 'MusicBrainz', configSchema: { fields: [{ key: 'path', label: 'Path', type: 'text', required: true, secret: false }] } })],
      isLoading: false,
    })
    wrap(<Admin />)
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    expect(screen.getByLabelText(/path/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(screen.queryByLabelText(/path/i)).toBeNull()
  })
})
