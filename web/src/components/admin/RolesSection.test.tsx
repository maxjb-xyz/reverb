import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RolesSection } from './RolesSection'

// ── Mocks ─────────────────────────────────────────────────────────────────────

vi.mock('../../lib/usersApi', () => ({
  useRoles: vi.fn(),
  useCapabilities: vi.fn(),
  createRole: vi.fn(),
  updateRole: vi.fn(),
  deleteRole: vi.fn(),
}))

import {
  useRoles,
  useCapabilities,
  createRole,
  updateRole,
  deleteRole,
} from '../../lib/usersApi'

// ── Helpers ───────────────────────────────────────────────────────────────────

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
}

// Real capability registry keys (mirrors GET /capabilities contract)
const CAPABILITIES = [
  { key: 'is_admin', label: 'Full administrator', description: 'Complete access; bypasses all restrictions. Opens the Admin area.' },
  { key: 'can_manage_users', label: 'Manage users & roles', description: 'Create and edit users, edit roles, and control registration & invites. Opens the Admin area.' },
  { key: 'can_manage_library', label: 'Manage library & integrations', description: 'Configure the music backend, search providers, and downloaders. Opens the Admin area.' },
  { key: 'request', label: 'Request music', description: 'Ask to add music to the library.' },
  { key: 'auto_approve', label: 'Auto-approve music', description: 'Requests to add music are fulfilled immediately, without approval.' },
  { key: 'can_create_playlists', label: 'Create & edit playlists', description: 'Make and manage their own playlists.' },
]

const SYSTEM_ROLE = {
  id: 'r-sys',
  name: 'Admin',
  isSystem: true,
  capabilities: ['is_admin', 'can_manage_users'],
}

const CUSTOM_ROLE = {
  id: 'r-custom',
  name: 'Reader',
  isSystem: false,
  capabilities: ['request'],
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('RolesSection', () => {
  beforeEach(() => {
    vi.mocked(useRoles).mockReturnValue({
      data: [SYSTEM_ROLE, CUSTOM_ROLE],
      isLoading: false,
    } as ReturnType<typeof useRoles>)
    vi.mocked(useCapabilities).mockReturnValue({
      data: CAPABILITIES,
      isLoading: false,
    } as ReturnType<typeof useCapabilities>)
    vi.mocked(createRole).mockReset()
    vi.mocked(createRole).mockResolvedValue(undefined as never)
    vi.mocked(updateRole).mockReset()
    vi.mocked(updateRole).mockResolvedValue(undefined as never)
    vi.mocked(deleteRole).mockReset()
    vi.mocked(deleteRole).mockResolvedValue(undefined as never)
  })

  it('renders both roles', () => {
    render(wrap(<RolesSection />))
    expect(screen.getByText('Admin')).toBeInTheDocument()
    expect(screen.getByText('Reader')).toBeInTheDocument()
  })

  // SP1 inverted: default (isSystem) roles are NOW editable
  it('default (isSystem) role shows an Edit button', () => {
    render(wrap(<RolesSection />))
    // With both roles editable there should be two Edit buttons
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    expect(editBtns.length).toBeGreaterThanOrEqual(2)
  })

  it('default (isSystem) role shows a "Default" badge (not "System")', () => {
    render(wrap(<RolesSection />))
    expect(screen.getByText('Default')).toBeInTheDocument()
    expect(screen.queryByText('System')).not.toBeInTheDocument()
  })

  it('custom role has an Edit button', () => {
    render(wrap(<RolesSection />))
    expect(screen.getAllByRole('button', { name: /^edit$/i }).length).toBeGreaterThanOrEqual(1)
  })

  it('clicking Edit on custom role opens the capability checklist', async () => {
    render(wrap(<RolesSection />))
    // Click the second Edit button (custom role is second)
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editBtns[editBtns.length - 1])
    // The capability checklist form should appear with checkboxes for each capability
    await waitFor(() => {
      // "Auto-approve music" doesn't appear as a role chip on the Reader role
      expect(screen.getByRole('checkbox', { name: /auto-approve music/i })).toBeInTheDocument()
    })
  })

  it('toggling a capability checkbox and saving calls updateRole', async () => {
    render(wrap(<RolesSection />))
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editBtns[editBtns.length - 1])

    await waitFor(() => {
      expect(screen.getByText('Auto-approve music')).toBeInTheDocument()
    })

    // Toggle "Auto-approve music" (currently unchecked for Reader role)
    const autoApproveCheckbox = screen.getByRole('checkbox', { name: /auto-approve music/i })
    fireEvent.click(autoApproveCheckbox)

    // Save
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(updateRole).toHaveBeenCalledWith('r-custom', {
        name: 'Reader',
        capabilities: expect.arrayContaining(['request', 'auto_approve']),
      })
    })
  })

  it('toggling a capability on a default (isSystem) role calls updateRole', async () => {
    render(wrap(<RolesSection />))
    // Click the first Edit button (system role is first)
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editBtns[0])

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /auto-approve music/i })).toBeInTheDocument()
    })

    const autoApproveCheckbox = screen.getByRole('checkbox', { name: /auto-approve music/i })
    fireEvent.click(autoApproveCheckbox)

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(updateRole).toHaveBeenCalledWith('r-sys', {
        name: 'Admin',
        capabilities: expect.arrayContaining(['is_admin', 'can_manage_users', 'auto_approve']),
      })
    })
  })

  it('capability descriptions render in the checklist when editing', async () => {
    render(wrap(<RolesSection />))
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editBtns[0])

    await waitFor(() => {
      // Description text from CAPABILITIES mock
      expect(screen.getByText('Ask to add music to the library.')).toBeInTheDocument()
    })
  })

  it('create role button opens the form and calls createRole on submit', async () => {
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /create role/i }))

    await waitFor(() => {
      // The create form should open with capability checkboxes
      expect(screen.getByRole('checkbox', { name: /auto-approve music/i })).toBeInTheDocument()
    })

    // Fill in name
    const nameInput = screen.getByRole('textbox', { name: /role name/i })
    fireEvent.change(nameInput, { target: { value: 'Moderator' } })

    // Check "Create & edit playlists"
    const playlistCheckbox = screen.getByRole('checkbox', { name: /create & edit playlists/i })
    fireEvent.click(playlistCheckbox)

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(createRole).toHaveBeenCalledWith({
        name: 'Moderator',
        capabilities: ['can_create_playlists'],
      })
    })
  })

  it('delete button calls deleteRole with the role id', async () => {
    render(wrap(<RolesSection />))
    // Both roles now have Delete buttons; click the first one (SYSTEM_ROLE, id: 'r-sys')
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteBtns[0])
    await waitFor(() => {
      expect(deleteRole).toHaveBeenCalledWith('r-sys')
    })
  })

  it('handles deleteRole 409 (role in use) with a friendly message', async () => {
    vi.mocked(deleteRole).mockRejectedValue(
      Object.assign(new Error('role is assigned to users'), { status: 409, body: { error: 'role is assigned to users' } }),
    )
    render(wrap(<RolesSection />))
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteBtns[0])
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/reassign/i)
    })
  })

  it('handles deleteRole 409 (last admin) with a friendly message', async () => {
    vi.mocked(deleteRole).mockRejectedValue(
      Object.assign(new Error('would leave no administrator'), { status: 409, body: { error: 'would leave no administrator' } }),
    )
    render(wrap(<RolesSection />))
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteBtns[0])
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/no administrator/i)
    })
  })

  it('handles deleteRole 409 (registration default) with a friendly message', async () => {
    vi.mocked(deleteRole).mockRejectedValue(
      Object.assign(new Error('registration default'), { status: 409, body: { error: 'registration default' } }),
    )
    render(wrap(<RolesSection />))
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    fireEvent.click(deleteBtns[0])
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/registration default/i)
    })
  })

  it('handles updateRole 409 (last admin) with a friendly message in the form', async () => {
    vi.mocked(updateRole).mockRejectedValue(
      Object.assign(new Error('would leave no administrator'), { status: 409, body: { error: 'would leave no administrator' } }),
    )
    render(wrap(<RolesSection />))
    const editBtns = screen.getAllByRole('button', { name: /^edit$/i })
    fireEvent.click(editBtns[0])

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^save$/i })).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/no administrator/i)
    })
  })
})
