import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Setup from './Setup'

vi.mock('../lib/api', () => ({ api: { post: vi.fn(() => Promise.resolve({ ok: true })), put: vi.fn(() => Promise.resolve({})) }, loginErrorMessage: vi.fn(() => 'Incorrect username or password') }))
vi.mock('../lib/adaptersApi', () => ({
  useAvailableAdapters: vi.fn(() => ({ data: [
    { type: 'library', name: 'subsonic', configSchema: { fields: [] }, capabilities: [] },
    { type: 'downloader', name: 'spotdl', configSchema: { fields: [] }, capabilities: [] },
  ] })),
  useAdapters: vi.fn(() => ({ data: [] })),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  updateAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: false })),
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
import { useAvailableAdapters, useAdapters, createAdapter } from '../lib/adaptersApi'

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
    expect(await screen.findByText(/choose a library/i)).toBeInTheDocument()
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
    await screen.findByText(/choose a library/i)

    fireEvent.click(screen.getByRole('button', { name: /use built-in/i }))
    expect(await screen.findByText(/couldn't save your library choice/i)).toBeInTheDocument()
    // Still on the library step (did not advance to the search step)
    expect(screen.getByText(/choose a library/i)).toBeInTheDocument()
  })

  it('library step offers a Built-in option that sets built-in mode and advances', async () => {
    renderSetup()
    // advance past password step
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await screen.findByText(/choose a library/i)

    fireEvent.click(screen.getByRole('button', { name: /use built-in/i }))
    await waitFor(() =>
      expect(api.put).toHaveBeenCalledWith('/settings', { libraryBackendMode: 'built-in' }),
    )
  })

  it('uses descriptive cards for built-in and external library choices', async () => {
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))

    expect(await screen.findByText('Recommended')).toBeInTheDocument()
    expect(screen.getByText(/reverb manages a music server/i)).toBeInTheDocument()
    expect(screen.getByText(/connect an existing navidrome/i)).toBeInTheDocument()
  })

  async function toLibraryStep() {
    renderSetup()
    fireEvent.change(screen.getByPlaceholderText('Choose a username'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    await screen.findByText(/choose a library/i)
  }

  it('walks library → search → downloads → done, and the unseeded downloader step lists its options', async () => {
    await toLibraryStep()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    await screen.findByText(/add a search source/i)
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))

    expect(await screen.findByRole('heading', { name: 'Downloads' })).toBeInTheDocument()
    expect(screen.getByText(/spotify credentials are optional/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    expect(await screen.findByText(/you're all set/i)).toBeInTheDocument()
  })

  it('Back returns to the previous step', async () => {
    await toLibraryStep()
    // The first step after the account has nothing to go back to.
    expect(screen.queryByRole('button', { name: 'Back' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    await screen.findByText(/add a search source/i)
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(await screen.findByText(/choose a library/i)).toBeInTheDocument()
  })

  it('a source that needs no settings (Deezer) is added in one click', async () => {
    vi.mocked(useAvailableAdapters).mockReturnValue({ data: [
      { type: 'search', name: 'deezer', configSchema: { fields: [] }, capabilities: [] },
      { type: 'search', name: 'spotify', configSchema: { fields: [{ key: 'client_id', label: 'Client ID', type: 'string', required: true, secret: false }] }, capabilities: [] },
    ] } as unknown as ReturnType<typeof useAvailableAdapters>)
    await toLibraryStep()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    await screen.findByText(/add a search source/i)

    fireEvent.click(screen.getByRole('button', { name: /Deezer/ }))
    await waitFor(() =>
      expect(createAdapter).toHaveBeenCalledWith({ type: 'search', name: 'deezer', enabled: true, priority: 0, config: {} }),
    )
    // No form in between: straight on to the next step.
    expect(await screen.findByRole('heading', { name: 'Downloads' })).toBeInTheDocument()
  })

  it('a source that needs settings still opens its form', async () => {
    vi.mocked(useAvailableAdapters).mockReturnValue({ data: [
      { type: 'search', name: 'spotify', configSchema: { fields: [{ key: 'client_id', label: 'Client ID', type: 'string', required: true, secret: false }] }, capabilities: [] },
    ] } as unknown as ReturnType<typeof useAvailableAdapters>)
    await toLibraryStep()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    await screen.findByText(/add a search source/i)
    fireEvent.click(screen.getByRole('button', { name: /Spotify/ }))
    expect(await screen.findByLabelText('Client ID')).toBeInTheDocument()
    expect(createAdapter).not.toHaveBeenCalled()
  })

  it('with the bundled downloader seeded, the downloads step is a confirmation with advanced options tucked away', async () => {
    vi.mocked(useAvailableAdapters).mockReturnValue({ data: [
      { type: 'downloader', name: 'spotdl', configSchema: { fields: [{ key: 'output_dir', label: 'Output', type: 'string', required: true, secret: false }] }, capabilities: [] },
      { type: 'downloader', name: 'lidarr', configSchema: { fields: [{ key: 'url', label: 'URL', type: 'string', required: true, secret: false }] }, capabilities: [] },
    ] } as unknown as ReturnType<typeof useAvailableAdapters>)
    vi.mocked(useAdapters).mockReturnValue({ data: [
      { id: 'd1', type: 'downloader', name: 'spotdl', enabled: true, priority: 0, config: { output_dir: '/music' }, capabilities: [] },
    ] } as unknown as ReturnType<typeof useAdapters>)
    await toLibraryStep()
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))
    await screen.findByText(/add a search source/i)
    fireEvent.click(screen.getByRole('button', { name: /skip this step/i }))

    expect(await screen.findByText('spotDL is ready')).toBeInTheDocument()
    expect(screen.getByText(/music is saved to \/music/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Lidarr/ })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Advanced options' }))
    expect(screen.getByRole('button', { name: /Lidarr/ })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(await screen.findByText(/you're all set/i)).toBeInTheDocument()
  })
})
