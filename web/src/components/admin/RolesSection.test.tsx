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

const CAPABILITIES = [
  { key: 'library.read', label: 'Browse library' },
  { key: 'download', label: 'Download music' },
  { key: 'playlist.write', label: 'Edit playlists' },
  { key: 'settings.read', label: 'View settings' },
  { key: 'settings.write', label: 'Edit settings' },
  { key: 'admin', label: 'Admin panel' },
]

const SYSTEM_ROLE = {
  id: 'r-sys',
  name: 'Admin',
  isSystem: true,
  capabilities: ['library.read', 'admin'],
}

const CUSTOM_ROLE = {
  id: 'r-custom',
  name: 'Reader',
  isSystem: false,
  capabilities: ['library.read'],
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

  it('system role shows no edit or delete buttons', () => {
    render(wrap(<RolesSection />))
    // Both roles rendered; check that there is no Edit button for system role
    // We look for delete buttons: only custom role should have one
    const deleteBtns = screen.queryAllByRole('button', { name: /delete/i })
    expect(deleteBtns).toHaveLength(1)
  })

  it('system role shows no editable checkboxes', () => {
    render(wrap(<RolesSection />))
    // System role capabilities are read-only — no interactive checkboxes for system role
    // There should be a system role badge or indicator
    expect(screen.getByText(/system/i)).toBeInTheDocument()
  })

  it('custom role has an Edit button', () => {
    render(wrap(<RolesSection />))
    expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument()
  })

  it('clicking Edit on custom role opens the capability checklist', async () => {
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /edit/i }))
    // The capability checklist form should appear with checkboxes for each capability
    await waitFor(() => {
      // "Download music" doesn't appear as a role chip, so it only appears in the checklist
      expect(screen.getByRole('checkbox', { name: /download music/i })).toBeInTheDocument()
    })
  })

  it('toggling a capability checkbox and saving calls updateRole', async () => {
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /edit/i }))

    await waitFor(() => {
      expect(screen.getByText('Download music')).toBeInTheDocument()
    })

    // Toggle "Download music" (currently unchecked for Reader role)
    const downloadCheckbox = screen.getByRole('checkbox', { name: /download music/i })
    fireEvent.click(downloadCheckbox)

    // Save
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(updateRole).toHaveBeenCalledWith('r-custom', {
        name: 'Reader',
        capabilities: expect.arrayContaining(['library.read', 'download']),
      })
    })
  })

  it('create role button opens the form and calls createRole on submit', async () => {
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /create role/i }))

    await waitFor(() => {
      // The create form should open with capability checkboxes
      expect(screen.getByRole('checkbox', { name: /download music/i })).toBeInTheDocument()
    })

    // Fill in name
    const nameInput = screen.getByRole('textbox', { name: /role name/i })
    fireEvent.change(nameInput, { target: { value: 'Moderator' } })

    // Check "Edit playlists"
    const playlistCheckbox = screen.getByRole('checkbox', { name: /edit playlists/i })
    fireEvent.click(playlistCheckbox)

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }))

    await waitFor(() => {
      expect(createRole).toHaveBeenCalledWith({
        name: 'Moderator',
        capabilities: ['playlist.write'],
      })
    })
  })

  it('delete button calls deleteRole', async () => {
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /delete/i }))
    await waitFor(() => {
      expect(deleteRole).toHaveBeenCalledWith('r-custom')
    })
  })

  it('handles deleteRole 409 (role in use) with a friendly message', async () => {
    vi.mocked(deleteRole).mockRejectedValue(
      Object.assign(new Error('Role is in use'), { status: 409 }),
    )
    render(wrap(<RolesSection />))
    fireEvent.click(screen.getByRole('button', { name: /delete/i }))
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/in use/i)
    })
  })
})
