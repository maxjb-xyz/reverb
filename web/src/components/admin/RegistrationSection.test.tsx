import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RegistrationSection } from './RegistrationSection'

// ── Mocks ─────────────────────────────────────────────────────────────────────

vi.mock('../../lib/usersApi', () => ({
  useRoles: vi.fn(),
  useRegistration: vi.fn(),
  setRegistration: vi.fn(),
  useInvites: vi.fn(),
  createInvite: vi.fn(),
  deleteInvite: vi.fn(),
}))

import {
  useRoles,
  useRegistration,
  setRegistration,
  useInvites,
  createInvite,
  deleteInvite,
} from '../../lib/usersApi'

// ── Helpers ───────────────────────────────────────────────────────────────────

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
}

const ROLES = [
  { id: 'r-1', name: 'Admin', isSystem: true, capabilities: [] as string[] },
  { id: 'r-2', name: 'User', isSystem: false, capabilities: [] as string[] },
]

const REG_INVITES_ENABLED = {
  signupEnabled: true,
  invitesEnabled: true,
  defaultRoleId: 'r-2',
}

const REG_INVITES_DISABLED = {
  signupEnabled: true,
  invitesEnabled: false,
  defaultRoleId: 'r-2',
}

const INVITE = {
  id: 'inv-1',
  code: 'ABC123',
  roleId: 'r-2',
  roleName: 'User', // backend now resolves & returns role name for invites with a roleId
  expiresAt: null,
  usedAt: null,
  createdAt: 1767225600, // 2026-01-01 UTC (Unix seconds — the real backend contract)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('RegistrationSection', () => {
  beforeEach(() => {
    vi.mocked(useRoles).mockReturnValue({
      data: ROLES,
      isLoading: false,
    } as ReturnType<typeof useRoles>)
    vi.mocked(useRegistration).mockReturnValue({
      data: REG_INVITES_ENABLED,
      isLoading: false,
    } as ReturnType<typeof useRegistration>)
    vi.mocked(useInvites).mockReturnValue({
      data: [INVITE],
      isLoading: false,
    } as ReturnType<typeof useInvites>)
    vi.mocked(setRegistration).mockReset()
    vi.mocked(setRegistration).mockResolvedValue(undefined as never)
    vi.mocked(createInvite).mockReset()
    vi.mocked(createInvite).mockResolvedValue({ code: 'NEW456', id: 'inv-new' } as never)
    vi.mocked(deleteInvite).mockReset()
    vi.mocked(deleteInvite).mockResolvedValue(undefined as never)
  })

  it('renders the Registration policy heading', () => {
    render(wrap(<RegistrationSection />))
    expect(screen.getByRole('heading', { name: /registration/i })).toBeInTheDocument()
  })

  it('shows signup and invites toggles', () => {
    render(wrap(<RegistrationSection />))
    expect(screen.getByRole('switch', { name: /signup/i })).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: /invites/i })).toBeInTheDocument()
  })

  it('toggling signup calls setRegistration with signupEnabled flipped', async () => {
    render(wrap(<RegistrationSection />))
    fireEvent.click(screen.getByRole('switch', { name: /signup/i }))
    await waitFor(() => {
      expect(setRegistration).toHaveBeenCalledWith(
        expect.objectContaining({ signupEnabled: false }),
      )
    })
  })

  it('toggling invites calls setRegistration with invitesEnabled flipped', async () => {
    render(wrap(<RegistrationSection />))
    fireEvent.click(screen.getByRole('switch', { name: /invites/i }))
    await waitFor(() => {
      expect(setRegistration).toHaveBeenCalledWith(
        expect.objectContaining({ invitesEnabled: false }),
      )
    })
  })

  it('shows the Invites area when invitesEnabled is true', () => {
    render(wrap(<RegistrationSection />))
    expect(screen.getByRole('heading', { name: /invites/i })).toBeInTheDocument()
  })

  it('hides the Invites area when invitesEnabled is false', () => {
    vi.mocked(useRegistration).mockReturnValue({
      data: REG_INVITES_DISABLED,
      isLoading: false,
    } as ReturnType<typeof useRegistration>)
    render(wrap(<RegistrationSection />))
    expect(screen.queryByRole('heading', { name: /invites/i })).not.toBeInTheDocument()
  })

  it('generating an invite calls createInvite and shows a copyable link', async () => {
    render(wrap(<RegistrationSection />))
    // Click generate invite
    fireEvent.click(screen.getByRole('button', { name: /generate invite/i }))
    await waitFor(() => {
      expect(createInvite).toHaveBeenCalled()
    })
    // The signup link should appear
    await waitFor(() => {
      expect(screen.getByText(/signup\?invite=NEW456/i)).toBeInTheDocument()
    })
  })

  it('lists existing invites', () => {
    render(wrap(<RegistrationSection />))
    // The invite code should appear in the list (may appear multiple times in code + url)
    const matches = screen.getAllByText(/ABC123/i)
    expect(matches.length).toBeGreaterThan(0)
  })

  it('revoking an invite calls deleteInvite', async () => {
    render(wrap(<RegistrationSection />))
    fireEvent.click(screen.getByRole('button', { name: /revoke/i }))
    await waitFor(() => {
      expect(deleteInvite).toHaveBeenCalledWith('inv-1')
    })
  })
})
