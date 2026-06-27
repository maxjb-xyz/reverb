import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Login from './Login'

vi.mock('../lib/session', () => ({
  login: vi.fn(() => Promise.resolve()),
}))
vi.mock('../lib/authStore', () => ({
  useAuthStore: { getState: vi.fn(() => ({ refresh: vi.fn(() => Promise.resolve()) })) },
}))
vi.mock('../lib/api', () => ({
  loginErrorMessage: vi.fn(() => 'Incorrect username or password'),
  api: { get: vi.fn(() => Promise.resolve({ signupEnabled: false, invitesEnabled: false })) },
}))
// react-router-dom navigate mock
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

import { login } from '../lib/session'

function renderLogin() {
  return render(
    <MemoryRouter>
      <Login />
    </MemoryRouter>,
  )
}

describe('Login page', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  it('renders a username and password field', () => {
    renderLogin()
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
  })

  it('calls login() with username and password on submit', async () => {
    renderLogin()
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /log in/i }))
    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'hunter2'))
  })
})
