import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { UsersSection } from './UsersSection'

// ── Mocks ─────────────────────────────────────────────────────────────────────

vi.mock('../../lib/usersApi', () => ({
  useUsers: vi.fn(),
  useRoles: vi.fn(),
  createUser: vi.fn(),
  updateUser: vi.fn(),
  deleteUser: vi.fn(),
  resetPassword: vi.fn(),
}))

import {
  useUsers,
  useRoles,
  createUser,
  updateUser,
  deleteUser,
} from '../../lib/usersApi'

// ── Helpers ───────────────────────────────────────────────────────────────────

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
}

const OWNER = {
  id: 'u-1',
  username: 'admin',
  roleId: 'r-1',
  roleName: 'Owner',
  isOwner: true,
  disabled: false,
  createdAt: '2025-01-01T00:00:00Z',
  lastSeen: '2025-06-01T10:00:00Z',
}

const BOB = {
  id: 'u-2',
  username: 'bob',
  roleId: 'r-2',
  roleName: 'User',
  isOwner: false,
  disabled: false,
  createdAt: '2025-02-01T00:00:00Z',
  lastSeen: null,
}

const ROLES = [
  { id: 'r-1', name: 'Owner' },
  { id: 'r-2', name: 'User' },
]

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('UsersSection', () => {
  beforeEach(() => {
    vi.mocked(useUsers).mockReturnValue({
      data: [OWNER, BOB],
      isLoading: false,
    } as ReturnType<typeof useUsers>)
    vi.mocked(useRoles).mockReturnValue({
      data: ROLES,
      isLoading: false,
    } as ReturnType<typeof useRoles>)
    vi.mocked(createUser).mockReset()
    vi.mocked(createUser).mockResolvedValue(undefined as never)
    vi.mocked(updateUser).mockReset()
    vi.mocked(updateUser).mockResolvedValue(undefined as never)
    vi.mocked(deleteUser).mockReset()
    vi.mocked(deleteUser).mockResolvedValue(undefined as never)
  })

  it('renders both user rows', () => {
    render(wrap(<UsersSection />))
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(screen.getByText('bob')).toBeInTheDocument()
  })

  it('shows a lock icon on the owner row', () => {
    render(wrap(<UsersSection />))
    expect(screen.getByTestId('owner-lock')).toBeInTheDocument()
  })

  it('owner row has NO delete button', () => {
    render(wrap(<UsersSection />))
    // There should be a delete button for bob but NOT for the owner row.
    const deleteBtns = screen.getAllByRole('button', { name: /delete/i })
    // Only bob's delete button should exist (owner is protected)
    expect(deleteBtns).toHaveLength(1)
  })

  it('owner row has NO disable/enable button', () => {
    render(wrap(<UsersSection />))
    const disableBtns = screen.queryAllByRole('button', { name: /disable|enable/i })
    // Only bob should have a disable button
    expect(disableBtns).toHaveLength(1)
  })

  it('shows loading skeletons when isLoading=true', () => {
    vi.mocked(useUsers).mockReturnValue({
      data: undefined,
      isLoading: true,
    } as unknown as ReturnType<typeof useUsers>)
    render(wrap(<UsersSection />))
    // Skeleton elements should be present
    expect(screen.getAllByTestId('user-skeleton').length).toBeGreaterThan(0)
  })

  it('shows empty state when there are no users', () => {
    vi.mocked(useUsers).mockReturnValue({
      data: [],
      isLoading: false,
    } as unknown as ReturnType<typeof useUsers>)
    render(wrap(<UsersSection />))
    expect(screen.getByText(/no users/i)).toBeInTheDocument()
  })

  describe('Create user modal', () => {
    it('"Create user" button opens the modal', () => {
      render(wrap(<UsersSection />))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('button', { name: /create user/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    it('submitting the create form calls createUser with username, password, roleId', async () => {
      render(wrap(<UsersSection />))
      fireEvent.click(screen.getByRole('button', { name: /create user/i }))

      // Scope to the dialog to avoid ambiguity with the table's role selects
      const dialog = screen.getByRole('dialog')

      fireEvent.change(screen.getByLabelText(/username/i), {
        target: { value: 'alice' },
      })
      fireEvent.change(screen.getByLabelText(/^password$/i), {
        target: { value: 'secret123' },
      })

      // Select "User" role (r-2) — scoped to dialog to avoid bob's row Select
      const roleSelect = dialog.querySelector('select[aria-label="Role"]') as HTMLSelectElement
      fireEvent.change(roleSelect, { target: { value: 'r-2' } })

      fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

      await waitFor(() => {
        expect(createUser).toHaveBeenCalledWith({
          username: 'alice',
          password: 'secret123',
          roleId: 'r-2',
        })
      })
    })

    it('Cancel closes the modal without calling createUser', () => {
      render(wrap(<UsersSection />))
      fireEvent.click(screen.getByRole('button', { name: /create user/i }))
      expect(screen.getByRole('dialog')).toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      expect(createUser).not.toHaveBeenCalled()
    })
  })
})
