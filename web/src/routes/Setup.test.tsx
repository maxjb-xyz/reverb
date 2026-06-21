import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Setup from './Setup'

vi.mock('../lib/api', () => ({ api: { post: vi.fn(() => Promise.resolve({ ok: true })) } }))
vi.mock('../lib/adaptersApi', () => ({
  useAvailableAdapters: vi.fn(() => ({ data: [{ type: 'library', name: 'subsonic', configSchema: { fields: [] }, capabilities: [] }] })),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  // testAdapter + SECRET_SENTINEL must be included: Setup imports AdapterForm which
  // imports { testAdapter, SECRET_SENTINEL } from adaptersApi. A full vi.mock factory
  // must export every symbol or Vitest throws "No 'testAdapter' export" at import time.
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))
import { api } from '../lib/api'

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

  it('step 1 prompts for an admin password', () => {
    renderSetup()
    expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('Choose a password')).toBeInTheDocument()
  })

  it('advances to the Library step after setting a password', async () => {
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await waitFor(() => expect(api.post).toHaveBeenCalledWith('/setup/admin', { password: 'hunter2' }))
    expect(await screen.findByText(/add a library/i)).toBeInTheDocument()
  })
})
