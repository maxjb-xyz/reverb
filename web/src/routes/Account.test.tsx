import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'

// ── Mock authStore ────────────────────────────────────────────────────────────
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

// ── Mock accountApi ────────────────────────────────────────────────────────────
const mockChangePassword = vi.fn()
const mockLogoutAll = vi.fn()

vi.mock('../lib/accountApi', () => ({
  changePassword: (...args: unknown[]) => mockChangePassword(...args),
  logoutAll: (...args: unknown[]) => mockLogoutAll(...args),
}))

import Account from './Account'

function wrap(ui: ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>)
}

describe('Account page', () => {
  beforeEach(() => {
    mockChangePassword.mockResolvedValue(undefined)
    mockLogoutAll.mockResolvedValue(undefined)
    mockLogout.mockResolvedValue(undefined)
  })
  afterEach(() => vi.clearAllMocks())

  // ── Profile section ───────────────────────────────────────────────────────
  it('renders the username', () => {
    wrap(<Account />)
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('renders the role name badge', () => {
    wrap(<Account />)
    expect(screen.getByText('Admin')).toBeInTheDocument()
  })

  it('renders capability chips for each capability', () => {
    wrap(<Account />)
    expect(screen.getByText('library:read')).toBeInTheDocument()
    expect(screen.getByText('library:write')).toBeInTheDocument()
    expect(screen.getByText('admin:users')).toBeInTheDocument()
  })

  it('renders "Member since" with the formatted join date', () => {
    wrap(<Account />)
    const expectedDate = new Date(mockMe.createdAt * 1000).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    })
    expect(screen.getByText(new RegExp(`Member since ${expectedDate}`))).toBeInTheDocument()
  })

  // ── Security: inline validation (new ≠ confirm) ───────────────────────────
  it('shows inline error when new and confirm passwords do not match', async () => {
    wrap(<Account />)
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'old' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'different' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    expect(await screen.findByText(/passwords do not match/i)).toBeInTheDocument()
  })

  it('does NOT call changePassword when new and confirm passwords do not match', async () => {
    wrap(<Account />)
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'old' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'different' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    // Wait briefly to ensure the mock was not called even asynchronously
    await waitFor(() => expect(mockChangePassword).not.toHaveBeenCalled())
  })

  // ── Security: successful submission ───────────────────────────────────────
  it('calls changePassword(current, next) when form is valid', async () => {
    wrap(<Account />)
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'oldpass' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass1' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'newpass1' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    await waitFor(() => expect(mockChangePassword).toHaveBeenCalledWith('oldpass', 'newpass1'))
  })

  it('shows success message after password change', async () => {
    wrap(<Account />)
    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: 'oldpass' } })
    fireEvent.change(screen.getByLabelText(/^new password/i), { target: { value: 'newpass1' } })
    fireEvent.change(screen.getByLabelText(/confirm.*password/i), { target: { value: 'newpass1' } })
    fireEvent.click(screen.getByRole('button', { name: /change password/i }))
    expect(await screen.findByText(/password changed/i)).toBeInTheDocument()
  })

  // ── Sessions ──────────────────────────────────────────────────────────────
  it('sign out button calls logout', async () => {
    wrap(<Account />)
    fireEvent.click(screen.getByRole('button', { name: /^sign out$/i }))
    await waitFor(() => expect(mockLogout).toHaveBeenCalled())
  })

  it('sign out everywhere calls logoutAll', async () => {
    wrap(<Account />)
    fireEvent.click(screen.getByRole('button', { name: /sign out everywhere/i }))
    await waitFor(() => expect(mockLogoutAll).toHaveBeenCalled())
  })
})
