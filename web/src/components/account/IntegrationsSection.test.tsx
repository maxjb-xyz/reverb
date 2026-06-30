import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { Me } from '../../lib/authStore'

// ── Mock scrobbleApi ──────────────────────────────────────────────────────────

const mockGetLinks = vi.fn()
const mockLastfmAuthUrl = vi.fn()
const mockLastfmComplete = vi.fn()
const mockLastfmDisconnect = vi.fn()
const mockGetLastfmConfig = vi.fn()
const mockSetLastfmConfig = vi.fn()

vi.mock('../../lib/scrobbleApi', () => ({
  getLinks: (...args: unknown[]) => mockGetLinks(...args),
  lastfmAuthUrl: (...args: unknown[]) => mockLastfmAuthUrl(...args),
  lastfmComplete: (...args: unknown[]) => mockLastfmComplete(...args),
  lastfmDisconnect: (...args: unknown[]) => mockLastfmDisconnect(...args),
  getLastfmConfig: (...args: unknown[]) => mockGetLastfmConfig(...args),
  setLastfmConfig: (...args: unknown[]) => mockSetLastfmConfig(...args),
  ScrobbleError: class ScrobbleError extends Error {
    code: string
    constructor(code: string, message: string) {
      super(message)
      this.name = 'ScrobbleError'
      this.code = code
    }
  },
}))

import { IntegrationsSection } from './IntegrationsSection'

// ── Test helpers ──────────────────────────────────────────────────────────────

function wrap(ui: ReactElement) {
  return render(ui)
}

const regularMe: Me = {
  id: 'u1',
  username: 'alice',
  roleId: 'role-user',
  roleName: 'User',
  isOwner: false,
  capabilities: ['request', 'can_create_playlists'],
  createdAt: 1700000000,
}

const adminMe: Me = {
  id: 'u2',
  username: 'admin',
  roleId: 'role-admin',
  roleName: 'Admin',
  isOwner: true,
  capabilities: ['is_admin', 'can_manage_library', 'can_manage_users', 'request', 'can_create_playlists'],
  createdAt: 1700000000,
}

beforeEach(() => {
  // Default: not configured, no links
  mockGetLinks.mockResolvedValue({ configured: false, links: [] })
  mockGetLastfmConfig.mockResolvedValue({ apiKey: '', apiSecretSet: false })
  mockLastfmAuthUrl.mockResolvedValue({ authUrl: 'https://last.fm/auth', token: 'tok123' })
  mockLastfmComplete.mockResolvedValue({ username: 'musicfan99' })
  mockLastfmDisconnect.mockResolvedValue(undefined)
  mockSetLastfmConfig.mockResolvedValue(undefined)
})

afterEach(() => vi.clearAllMocks())

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('IntegrationsSection', () => {
  // Test 1: configured===false → shows "not set up" message, no Connect button
  it('shows "not set up on this server yet" when configured is false', async () => {
    mockGetLinks.mockResolvedValue({ configured: false, links: [] })
    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByText(/set up on this server yet/i)).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /connect last\.fm/i })).not.toBeInTheDocument()
  })

  // Test 2: configured===true, no link → shows "Connect Last.fm" button
  it('shows Connect Last.fm button when configured but no link', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })
  })

  // Test 3: Clicking Connect calls lastfmAuthUrl, opens window, shows "I've approved"
  it('clicking Connect Last.fm calls lastfmAuthUrl and shows approval button', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    const mockOpen = vi.fn()
    vi.stubGlobal('open', mockOpen)

    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))

    await waitFor(() => {
      expect(mockLastfmAuthUrl).toHaveBeenCalled()
      expect(mockOpen).toHaveBeenCalledWith('https://last.fm/auth', '_blank')
      expect(screen.getByRole('button', { name: /i've approved/i })).toBeInTheDocument()
    })

    vi.unstubAllGlobals()
  })

  // Test 4: Clicking "I've approved" calls lastfmComplete(token), shows "Connected as <username>"
  it('clicking I\'ve approved completes auth and shows connected username', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    vi.stubGlobal('open', vi.fn())

    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /i've approved/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /i've approved/i }))
    await waitFor(() => {
      expect(mockLastfmComplete).toHaveBeenCalledWith('tok123')
      expect(screen.getByText(/connected as musicfan99/i)).toBeInTheDocument()
    })

    vi.unstubAllGlobals()
  })

  // Test 5: lastfmAuthUrl returning lastfm_unavailable → shows "temporarily unavailable"
  it('shows temporarily unavailable message on lastfm_unavailable error', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    // Import the ScrobbleError from the mock
    const { ScrobbleError } = await import('../../lib/scrobbleApi')
    mockLastfmAuthUrl.mockRejectedValue(new ScrobbleError('lastfm_unavailable', 'unavailable'))

    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))
    await waitFor(() => {
      expect(screen.getByText(/temporarily unavailable/i)).toBeInTheDocument()
    })
    // Must NOT show the admin configuration message
    expect(screen.queryByText(/contact.*admin/i)).not.toBeInTheDocument()
  })

  // Test 6: link status "active" → shows "Connected as <username>" and "Disconnect" button
  it('shows connected state and Disconnect button for active link', async () => {
    mockGetLinks.mockResolvedValue({
      configured: true,
      links: [{ provider: 'lastfm', username: 'testuser', status: 'active' }],
    })
    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByText(/connected as testuser/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /disconnect/i })).toBeInTheDocument()
    })
  })

  // Test 7: Clicking Disconnect calls lastfmDisconnect, returns to not-linked state
  it('clicking Disconnect calls lastfmDisconnect and shows Connect button', async () => {
    mockGetLinks.mockResolvedValue({
      configured: true,
      links: [{ provider: 'lastfm', username: 'testuser', status: 'active' }],
    })
    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /disconnect/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /disconnect/i }))
    await waitFor(() => {
      expect(mockLastfmDisconnect).toHaveBeenCalled()
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })
  })

  // Test 8: link status "broken" → shows "needs reconnecting" + "Reconnect" button
  it('shows needs reconnecting and Reconnect button for broken link', async () => {
    mockGetLinks.mockResolvedValue({
      configured: true,
      links: [{ provider: 'lastfm', username: 'testuser', status: 'broken' }],
    })
    wrap(<IntegrationsSection me={regularMe} />)
    await waitFor(() => {
      expect(screen.getByText(/needs reconnecting/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /reconnect/i })).toBeInTheDocument()
    })
  })

  // Test 9: Admin subsection shown only for can_manage_library / is_admin
  it('shows admin subsection for users with can_manage_library', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    mockGetLastfmConfig.mockResolvedValue({ apiKey: 'mykey', apiSecretSet: false })
    wrap(<IntegrationsSection me={adminMe} />)
    await waitFor(() => {
      expect(screen.getByText(/app configuration/i)).toBeInTheDocument()
    })
  })

  it('does NOT show admin subsection for regular users', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    wrap(<IntegrationsSection me={regularMe} />)
    // Wait for the component to finish loading (Connect button appears when configured=true)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })
    expect(screen.queryByText(/app configuration/i)).not.toBeInTheDocument()
  })

  // Test 10: Admin save calls setLastfmConfig
  it('admin save button calls setLastfmConfig with apiKey and apiSecret', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    mockGetLastfmConfig.mockResolvedValue({ apiKey: 'existing-key', apiSecretSet: true })
    wrap(<IntegrationsSection me={adminMe} />)

    await waitFor(() => {
      expect(screen.getByText(/app configuration/i)).toBeInTheDocument()
    })

    // Fill in a new API key
    const apiKeyInput = screen.getByPlaceholderText(/api key/i)
    fireEvent.change(apiKeyInput, { target: { value: 'new-api-key' } })

    // Click save
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    await waitFor(() => {
      expect(mockSetLastfmConfig).toHaveBeenCalledWith(
        expect.objectContaining({ apiKey: 'new-api-key' }),
      )
    })
  })
})
