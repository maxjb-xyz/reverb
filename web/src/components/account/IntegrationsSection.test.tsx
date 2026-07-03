import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'

// ── Mock scrobbleApi ──────────────────────────────────────────────────────────

const mockGetLinks = vi.fn()
const mockLastfmAuthUrl = vi.fn()
const mockLastfmComplete = vi.fn()
const mockLastfmDisconnect = vi.fn()

vi.mock('../../lib/scrobbleApi', () => ({
  getLinks: (...args: unknown[]) => mockGetLinks(...args),
  lastfmAuthUrl: (...args: unknown[]) => mockLastfmAuthUrl(...args),
  lastfmComplete: (...args: unknown[]) => mockLastfmComplete(...args),
  lastfmDisconnect: (...args: unknown[]) => mockLastfmDisconnect(...args),
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

beforeEach(() => {
  // Default: not configured, no links
  mockGetLinks.mockResolvedValue({ configured: false, links: [] })
  mockLastfmAuthUrl.mockResolvedValue({ authUrl: 'https://last.fm/auth', token: 'tok123' })
  mockLastfmComplete.mockResolvedValue({ username: 'musicfan99' })
  mockLastfmDisconnect.mockResolvedValue(undefined)
})

afterEach(() => vi.clearAllMocks())

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('IntegrationsSection', () => {
  // Test 1: configured===false → shows "not set up" message, no Connect button
  it('shows "not set up on this server yet" when configured is false', async () => {
    mockGetLinks.mockResolvedValue({ configured: false, links: [] })
    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByText(/set up on this server yet/i)).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /connect last\.fm/i })).not.toBeInTheDocument()
  })

  // Test 2: configured===true, no link → shows "Connect Last.fm" button
  it('shows Connect Last.fm button when configured but no link', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })
  })

  // Test 3: Clicking Connect calls lastfmAuthUrl, opens window, shows "I've approved"
  it('clicking Connect Last.fm calls lastfmAuthUrl and shows approval button', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    const mockOpen = vi.fn()
    vi.stubGlobal('open', mockOpen)

    wrap(<IntegrationsSection />)
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
  it("clicking I've approved completes auth and shows connected username", async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    vi.stubGlobal('open', vi.fn())

    wrap(<IntegrationsSection />)
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

  // Test 5: lastfmAuthUrl returning lastfm_unavailable → shows the TRANSIENT message,
  // and NOT the admin-oriented "not set up" message.
  it('shows the transient "temporarily unavailable" message on lastfm_unavailable (not the admin message)', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    const { ScrobbleError } = await import('../../lib/scrobbleApi')
    mockLastfmAuthUrl.mockRejectedValue(new ScrobbleError('lastfm_unavailable', 'unavailable'))

    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))
    await waitFor(() => {
      expect(
        screen.getByText('Last.fm is temporarily unavailable — try again.'),
      ).toBeInTheDocument()
    })
    // Must NOT show the admin-oriented "not set up" message for the transient code.
    expect(screen.queryByText(/isn't set up on this server yet/i)).not.toBeInTheDocument()
  })

  // Test 5b: lastfmAuthUrl returning lastfm_not_configured → shows the ADMIN-oriented
  // message, which is DIFFERENT from the transient message.
  it('shows a distinct admin-oriented message on lastfm_not_configured', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    const { ScrobbleError } = await import('../../lib/scrobbleApi')
    mockLastfmAuthUrl.mockRejectedValue(
      new ScrobbleError('lastfm_not_configured', 'not configured'),
    )

    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))
    await waitFor(() => {
      expect(
        screen.getByText(
          "Last.fm isn't set up on this server yet — ask an administrator to configure it.",
        ),
      ).toBeInTheDocument()
    })
    // Must NOT show the transient message for the not-configured code.
    expect(screen.queryByText(/temporarily unavailable/i)).not.toBeInTheDocument()
  })

  // Test 6: link status "active" → shows "Connected as <username>" and "Disconnect" button
  it('shows connected state and Disconnect button for active link', async () => {
    mockGetLinks.mockResolvedValue({
      configured: true,
      links: [{ provider: 'lastfm', username: 'testuser', status: 'active' }],
    })
    wrap(<IntegrationsSection />)
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
    wrap(<IntegrationsSection />)
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
    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByText(/needs reconnecting/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /reconnect/i })).toBeInTheDocument()
    })
  })

  // Test 8b: Clicking Reconnect on a broken link must REACH the finish-connecting step.
  it('clicking Reconnect on a broken link opens authUrl and reveals the finish button', async () => {
    mockGetLinks.mockResolvedValue({
      configured: true,
      links: [{ provider: 'lastfm', username: 'testuser', status: 'broken' }],
    })
    const mockOpen = vi.fn()
    vi.stubGlobal('open', mockOpen)

    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /reconnect/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /reconnect/i }))

    await waitFor(() => {
      expect(mockLastfmAuthUrl).toHaveBeenCalled()
      expect(mockOpen).toHaveBeenCalledWith('https://last.fm/auth', '_blank')
      expect(screen.getByRole('button', { name: /i've approved/i })).toBeInTheDocument()
    })
    expect(screen.queryByRole('button', { name: /reconnect/i })).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  // Test 8c: A rejecting lastfmComplete must recover.
  it('recovers when lastfmComplete rejects (shows error + a retry affordance)', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    vi.stubGlobal('open', vi.fn())
    mockLastfmComplete.mockRejectedValue(new Error('boom'))

    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /connect last\.fm/i }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /i've approved/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /i've approved/i }))

    await waitFor(() => {
      expect(screen.getByText(/couldn't finish connecting/i)).toBeInTheDocument()
    })
    const retry = screen.getByRole('button', { name: /try again/i })
    expect(retry).toBeEnabled()

    vi.unstubAllGlobals()
  })

  // Test 8d: getLinks() failure must surface an error state.
  it('shows an error state (not infinite Loading) when getLinks fails', async () => {
    mockGetLinks.mockRejectedValue(new Error('network'))

    wrap(<IntegrationsSection />)

    await waitFor(() => {
      expect(screen.getByText(/couldn't load your integrations/i)).toBeInTheDocument()
    })
    expect(screen.queryByText(/loading/i)).not.toBeInTheDocument()
  })

  // ── Admin subsection MUST NOT appear in the Account page ────────────────────
  // The Last.fm app config now lives at /admin (ScrobblingSection), not here.

  it('does NOT show admin "App configuration" section (moved to /admin)', async () => {
    mockGetLinks.mockResolvedValue({ configured: true, links: [] })
    wrap(<IntegrationsSection />)
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /connect last\.fm/i })).toBeInTheDocument()
    })
    expect(screen.queryByText(/app configuration/i)).not.toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/api key/i)).not.toBeInTheDocument()
  })
})
