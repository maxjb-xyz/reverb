/**
 * Settings page — unified per-user settings (Profile, Security, Sessions,
 * Integrations, Appearance). These tests replace the old split Account + Settings
 * test files.
 *
 * TDD order: tests written BEFORE implementation so every test must first fail.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { AdapterInstance } from '../lib/adaptersApi'

// ── Auth mock ─────────────────────────────────────────────────────────────────
const mockLogout = vi.fn()
const mockMe = {
  id: 'u1',
  username: 'alice',
  roleId: 'r1',
  roleName: 'Admin',
  isOwner: true,
  capabilities: ['library:read', 'library:write', 'admin:users'],
  createdAt: 1700000000,
}

vi.mock('../lib/authStore', () => ({
  useAuthStore: (selector: (s: { me: typeof mockMe; logout: typeof mockLogout }) => unknown) =>
    selector({ me: mockMe, logout: mockLogout }),
}))

// ── Account API mock ──────────────────────────────────────────────────────────
const mockChangePassword = vi.fn()
const mockLogoutAll = vi.fn()

vi.mock('../lib/accountApi', () => ({
  changePassword: (...args: unknown[]) => mockChangePassword(...args),
  logoutAll: (...args: unknown[]) => mockLogoutAll(...args),
}))

// ── Scrobble (Last.fm) mock ───────────────────────────────────────────────────
vi.mock('../lib/scrobbleApi', () => ({
  getLinks: vi.fn(async () => ({ configured: true, links: [] })),
  lastfmAuthUrl: vi.fn(async () => ({ authUrl: 'https://last.fm/auth', token: 'tok' })),
  lastfmComplete: vi.fn(async () => ({ username: 'lastfmuser' })),
  lastfmDisconnect: vi.fn(async () => undefined),
  getLastfmConfig: vi.fn(async () => ({ apiKey: '', apiSecretSet: false })),
  setLastfmConfig: vi.fn(async () => undefined),
  ScrobbleError: class ScrobbleError extends Error {
    code: string
    constructor(code: string, message: string) {
      super(message)
      this.name = 'ScrobbleError'
      this.code = code
    }
  },
}))

// ── Settings API mock ─────────────────────────────────────────────────────────
const mockMutate = vi.fn()
const mockUpdateAdapter = vi.fn(() => Promise.resolve({ data: {}, pendingRestart: false }))
const mockUseAdapters = vi.fn(() => ({ data: [] as AdapterInstance[] }))

vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({
    data: { accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' },
  })),
  useUpdateSettings: vi.fn(() => ({ mutate: mockMutate })),
  putSettings: vi.fn(() =>
    Promise.resolve({ accentColor: '#F0354B', dynamicBackground: true, libraryBackendMode: 'built-in' }),
  ),
  applyAccent: vi.fn(),
}))

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: () => mockUseAdapters(),
  updateAdapter: (...args: Parameters<typeof mockUpdateAdapter>) => mockUpdateAdapter(...args),
}))

import Settings from './Settings'

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  )
}

// ── Tab bar ───────────────────────────────────────────────────────────────────
describe('Settings — tab bar', () => {
  beforeEach(() => {
    mockChangePassword.mockResolvedValue(undefined)
    mockLogoutAll.mockResolvedValue(undefined)
    mockLogout.mockResolvedValue(undefined)
    mockMutate.mockClear()
  })
  afterEach(() => vi.clearAllMocks())

  it('renders a page heading "Settings"', () => {
    wrap(<Settings />)
    expect(screen.getByRole('heading', { name: /^settings$/i })).toBeInTheDocument()
  })

  it('renders all 5 tab buttons', () => {
    wrap(<Settings />)
    expect(screen.getByRole('button', { name: /^profile$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^security$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^sessions$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^integrations$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^appearance$/i })).toBeInTheDocument()
  })

  it('defaults to the Profile tab on first render', () => {
    wrap(<Settings />)
    // Profile content visible
    expect(screen.getByText('alice')).toBeInTheDocument()
    // Security content NOT visible
    expect(screen.queryByLabelText(/current password/i)).not.toBeInTheDocument()
  })

  it('clicking Security tab shows the security panel and hides profile panel', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^security$/i }))
    expect(screen.getByLabelText(/current password/i)).toBeInTheDocument()
    expect(screen.queryByText('alice')).not.toBeInTheDocument()
  })

  it('clicking Sessions tab shows sign-out buttons and hides profile panel', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^sessions$/i }))
    expect(screen.getByRole('button', { name: /^sign out$/i })).toBeInTheDocument()
    expect(screen.queryByText('alice')).not.toBeInTheDocument()
  })

  it('clicking Integrations tab shows Last.fm affordance and hides profile panel', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^integrations$/i }))
    // Last.fm heading is rendered (possibly multiple text nodes, use getAllByText)
    expect(screen.getAllByText(/last\.fm/i).length).toBeGreaterThan(0)
    expect(screen.queryByText('alice')).not.toBeInTheDocument()
  })

  it('clicking Appearance tab shows accent swatches and hides profile panel', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^appearance$/i }))
    expect(screen.getByRole('button', { name: /red \(default\)/i })).toBeInTheDocument()
    expect(screen.queryByText('alice')).not.toBeInTheDocument()
  })
})

// ── Profile tab ───────────────────────────────────────────────────────────────
describe('Settings — Profile tab', () => {
  beforeEach(() => {
    mockLogout.mockResolvedValue(undefined)
  })
  afterEach(() => vi.clearAllMocks())

  it('renders the username', () => {
    wrap(<Settings />)
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('renders the role name badge', () => {
    wrap(<Settings />)
    expect(screen.getByText('Admin')).toBeInTheDocument()
  })

  it('renders capability chips for each capability', () => {
    wrap(<Settings />)
    expect(screen.getByText('library:read')).toBeInTheDocument()
    expect(screen.getByText('library:write')).toBeInTheDocument()
    expect(screen.getByText('admin:users')).toBeInTheDocument()
  })

  it('renders "Member since" with the formatted join date', () => {
    wrap(<Settings />)
    const expectedDate = new Date(mockMe.createdAt * 1000).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    })
    expect(screen.getByText(new RegExp(`Member since ${expectedDate}`))).toBeInTheDocument()
  })
})

// ── Security tab ──────────────────────────────────────────────────────────────
describe('Settings — Security tab', () => {
  beforeEach(() => {
    mockChangePassword.mockResolvedValue(undefined)
  })
  afterEach(() => vi.clearAllMocks())

  function openSecurityTab() {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^security$/i }))
  }

  it('shows inline error when new and confirm passwords do not match', async () => {
    openSecurityTab()
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'old' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'different' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    expect(await screen.findByText(/passwords do not match/i)).toBeInTheDocument()
  })

  it('does NOT call changePassword when passwords do not match', async () => {
    openSecurityTab()
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'old' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'different' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    await waitFor(() => expect(mockChangePassword).not.toHaveBeenCalled())
  })

  it('calls changePassword(current, next) when form is valid', async () => {
    openSecurityTab()
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'oldpass' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass1' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'newpass1' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    await waitFor(() => expect(mockChangePassword).toHaveBeenCalledWith('oldpass', 'newpass1'))
  })

  it('shows success message after password change', async () => {
    openSecurityTab()
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'oldpass' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass1' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'newpass1' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    expect(await screen.findByText(/password changed/i)).toBeInTheDocument()
  })
})

// ── Sessions tab ──────────────────────────────────────────────────────────────
describe('Settings — Sessions tab', () => {
  beforeEach(() => {
    mockLogout.mockResolvedValue(undefined)
    mockLogoutAll.mockResolvedValue(undefined)
  })
  afterEach(() => vi.clearAllMocks())

  function openSessionsTab() {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^sessions$/i }))
  }

  it('sign out button calls logout', async () => {
    openSessionsTab()
    fireEvent.click(screen.getByRole('button', { name: /^sign out$/i }))
    await waitFor(() => expect(mockLogout).toHaveBeenCalled())
  })

  it('sign out everywhere calls logoutAll', async () => {
    openSessionsTab()
    fireEvent.click(screen.getByRole('button', { name: /sign out everywhere/i }))
    await waitFor(() => expect(mockLogoutAll).toHaveBeenCalled())
  })
})

// ── Integrations tab ──────────────────────────────────────────────────────────
describe('Settings — Integrations tab', () => {
  afterEach(() => vi.clearAllMocks())

  it('renders the Integrations heading under the Integrations tab', () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^integrations$/i }))
    expect(screen.getByRole('heading', { name: /integrations/i })).toBeInTheDocument()
  })

  it('shows the Connect Last.fm button (server configured)', async () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^integrations$/i }))
    expect(await screen.findByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
  })
})

// ── Appearance tab ────────────────────────────────────────────────────────────
describe('Settings — Appearance tab', () => {
  beforeEach(() => {
    mockMutate.mockClear()
  })
  afterEach(() => vi.clearAllMocks())

  function openAppearanceTab() {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /^appearance$/i }))
  }

  it('shows the accent swatches on the Appearance tab', () => {
    openAppearanceTab()
    expect(screen.getByRole('button', { name: /red \(default\)/i })).toBeInTheDocument()
  })

  it('shows the dynamic background toggle on the Appearance tab', () => {
    openAppearanceTab()
    expect(screen.getByRole('switch', { name: /dynamic album background/i })).toBeInTheDocument()
  })

  it('toggling dynamic background calls useUpdateSettings mutate', async () => {
    openAppearanceTab()
    const toggle = screen.getByRole('switch', { name: /dynamic album background/i })
    fireEvent.click(toggle)
    await waitFor(() => expect(mockMutate).toHaveBeenCalledWith({ dynamicBackground: false }))
  })

  it('NO adapter UI present — no Add library button', () => {
    openAppearanceTab()
    expect(screen.queryByRole('button', { name: /add library/i })).toBeNull()
  })

  it('does NOT render a "Default downloader" control', () => {
    openAppearanceTab()
    expect(screen.queryByLabelText('Default downloader')).not.toBeInTheDocument()
    expect(screen.queryByText(/default downloader/i)).not.toBeInTheDocument()
  })
})

// ── No dead admin/downloader UI ───────────────────────────────────────────────
describe('Settings — no admin cruft', () => {
  afterEach(() => vi.clearAllMocks())

  it('does NOT render a Downloaders heading', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('heading', { name: /downloaders/i })).not.toBeInTheDocument()
  })

  it('does NOT render a Remove button (no adapter UI)', () => {
    wrap(<Settings />)
    expect(screen.queryByRole('button', { name: /remove/i })).toBeNull()
  })
})
