import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
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
import { useAdapters, useAvailableAdapters, usePendingRestart, deleteAdapter } from '../lib/adaptersApi'

function wrap(ui: React.ReactElement) {
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
    expect(screen.getAllByText(/spotify/i).length).toBeGreaterThan(0)
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
})
