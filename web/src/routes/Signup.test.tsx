import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Signup from './Signup'

vi.mock('../lib/session', () => ({
  signup: vi.fn(() => Promise.resolve()),
}))
vi.mock('../lib/authStore', () => ({
  useAuthStore: { getState: vi.fn(() => ({ refresh: vi.fn(() => Promise.resolve()) })) },
}))
vi.mock('../lib/api', () => ({
  ApiError: class ApiError extends Error {
    status: number
    constructor(method: string, path: string, status: number) {
      super(`${method} ${path} -> ${status}`)
      this.name = 'ApiError'
      this.status = status
    }
  },
  api: { get: vi.fn(() => Promise.resolve({ signupEnabled: true, invitesEnabled: false })) },
}))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

import { signup } from '../lib/session'

function renderSignup(search = '') {
  return render(
    <MemoryRouter initialEntries={['/signup' + search]}>
      <Signup />
    </MemoryRouter>,
  )
}

describe('Signup page', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  it('renders username and password fields', () => {
    renderSignup()
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument()
  })

  it('calls signup() with username and password on submit', async () => {
    renderSignup()
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'alice' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'alicepw1' } })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))
    await waitFor(() => expect(signup).toHaveBeenCalledWith('alice', 'alicepw1', undefined))
  })

  it('passes the invite code from ?invite=CODE query param to signup()', async () => {
    renderSignup('?invite=TESTCODE123')
    fireEvent.change(screen.getByLabelText(/username/i), { target: { value: 'bob' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'bobpw123' } })
    fireEvent.click(screen.getByRole('button', { name: /create account/i }))
    await waitFor(() => expect(signup).toHaveBeenCalledWith('bob', 'bobpw123', 'TESTCODE123'))
  })
})
