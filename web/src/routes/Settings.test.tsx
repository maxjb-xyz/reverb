import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { ReactElement } from 'react'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Settings from './Settings'

vi.mock('../lib/adaptersApi', () => ({
  useAdapters: vi.fn(),
  useAvailableAdapters: vi.fn(),
  usePendingRestart: vi.fn(),
  createAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  updateAdapter: vi.fn(() => Promise.resolve({ data: {}, pendingRestart: true })),
  deleteAdapter: vi.fn(() => Promise.resolve({ ok: true, pendingRestart: true })),
  // testAdapter must be included: AdapterForm (imported by Settings) uses it, and a
  // full vi.mock factory must export EVERY symbol the module's consumers import or
  // Vitest throws "No 'testAdapter' export" at import time.
  testAdapter: vi.fn(() => Promise.resolve({ ok: true })),
  SECRET_SENTINEL: '••••••••',
}))
vi.mock('../lib/settingsApi', () => ({
  useSettings: vi.fn(() => ({ data: { accentColor: '#F0354B', dynamicBackground: true } })),
  putSettings: vi.fn(() => Promise.resolve({ accentColor: '#F0354B', dynamicBackground: true })),
  applyAccent: vi.fn(),
}))
import { useAdapters, useAvailableAdapters, usePendingRestart, deleteAdapter, createAdapter } from '../lib/adaptersApi'
import { putSettings, applyAccent } from '../lib/settingsApi'

function wrap(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('Settings', () => {
  beforeEach(() => {
    vi.mocked(useAvailableAdapters).mockReturnValue({ data: [{ type: 'search', name: 'spotify', configSchema: { fields: [] }, capabilities: [] }] } as unknown as ReturnType<typeof useAvailableAdapters>)
    vi.mocked(usePendingRestart).mockReturnValue({ data: { pendingRestart: false } } as unknown as ReturnType<typeof usePendingRestart>)
    vi.mocked(useAdapters).mockReturnValue({
      data: [{ id: 'a1', type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret__isSet: true } }],
    } as unknown as ReturnType<typeof useAdapters>)
  })
  afterEach(() => vi.clearAllMocks())

  it('lists configured instances', () => {
    wrap(<Settings />)
    // The configured instance renders as a listitem (<li>) with a Remove control,
    // distinct from the "+ Add spotify" button. Scope the assertion to that row so
    // it can't be satisfied by the Add button alone.
    const row = screen.getByRole('button', { name: /remove a1/i }).closest('li')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText(/spotify/i)).toBeInTheDocument()
  })

  it('shows the restart banner when pending', () => {
    vi.mocked(usePendingRestart).mockReturnValue({ data: { pendingRestart: true } } as unknown as ReturnType<typeof usePendingRestart>)
    wrap(<Settings />)
    expect(screen.getByText(/restart crate to apply/i)).toBeInTheDocument()
  })

  it('removes an instance', async () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /remove a1/i }))
    await waitFor(() => expect(deleteAdapter).toHaveBeenCalledWith('a1'))
  })

  it('persists and live-applies the accent color on change', async () => {
    wrap(<Settings />)
    const input = screen.getByLabelText(/accent color/i)
    fireEvent.change(input, { target: { value: '#00ff00' } })
    // The new colour is applied live (no reload) AND persisted to the backend.
    expect(applyAccent).toHaveBeenCalledWith('#00ff00')
    await waitFor(() => expect(putSettings).toHaveBeenCalledWith({ accentColor: '#00ff00' }))
  })

  it('adds an adapter through the add form', async () => {
    wrap(<Settings />)
    fireEvent.click(screen.getByRole('button', { name: /add spotify/i }))
    // AdapterForm appears with an "Add" submit button (no fields for this schema).
    const submit = await screen.findByRole('button', { name: /^add$/i })
    fireEvent.click(submit)
    await waitFor(() =>
      expect(createAdapter).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'search', name: 'spotify', enabled: true }),
      ),
    )
  })
})
