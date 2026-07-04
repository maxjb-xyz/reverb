import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Setup from './Setup'

vi.mock('../lib/api', () => ({ api: { post: vi.fn(() => Promise.resolve({ ok: true })), put: vi.fn(() => Promise.resolve({})) }, loginErrorMessage: vi.fn(() => 'Incorrect username or password') }))
vi.mock('../lib/adaptersApi', () => ({
  useAvailableAdapters: vi.fn(() => ({ data: [{ type: 'library', name: 'subsonic', configSchema: { fields: [] }, capabilities: [] }] })),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  // testAdapter + SECRET_SENTINEL must be included: Setup imports AdapterForm which
  // imports { testAdapter, SECRET_SENTINEL } from adaptersApi. A full vi.mock factory
  // must export every symbol or Vitest throws "No 'testAdapter' export" at import time.
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))
vi.mock('../lib/session', () => ({
  setupOwner: vi.fn(() => Promise.resolve()),
}))
vi.mock('../lib/authStore', () => ({
  useAuthStore: { getState: vi.fn(() => ({ refresh: vi.fn(() => Promise.resolve()) })) },
}))
import { api } from '../lib/api'
import { setupOwner } from '../lib/session'

function renderSetup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Setup />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Setup wizard', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  it('step 1 prompts for a username and admin password', () => {
    renderSetup()
    expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Choose a username')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Choose a password')).toBeInTheDocument()
  })

  it('advances to the Library step after setting a username + password, calling setupOwner with both', async () => {
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => expect(setupOwner).toHaveBeenCalledWith('admin', 'hunter2'))
    expect(await screen.findByText(/add a library/i)).toBeInTheDocument()
  })

  it('blocks the password step with a message when username or password is empty', async () => {
    renderSetup()
    // Submit with both empty
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(await screen.findByText(/enter a username and password/i)).toBeInTheDocument()
    expect(setupOwner).not.toHaveBeenCalled()

    // Username only — still blocked
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    expect(setupOwner).not.toHaveBeenCalled()
  })

  it('surfaces an error (and does not advance) when the library PUT fails', async () => {
    ;(api.put as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error('boom'))
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await screen.findByText(/add a library/i)

    fireEvent.click(screen.getByRole('button', { name: /use built-in/i }))
    expect(await screen.findByText(/couldn't save your library choice/i)).toBeInTheDocument()
    // Still on the library step (did not advance to the search step)
    expect(screen.getByText(/add a library/i)).toBeInTheDocument()
  })

  it('library step offers a Built-in option that sets built-in mode and advances', async () => {
    renderSetup()
    // advance past password step
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await screen.findByText(/add a library/i)

    fireEvent.click(screen.getByRole('button', { name: /use built-in/i }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/settings', { libraryBackendMode: 'built-in' }),
    )
  })
})
