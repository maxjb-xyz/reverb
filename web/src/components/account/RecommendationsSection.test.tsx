import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const getTasteFeedback = vi.fn()
const del = vi.fn()
vi.mock('../../lib/api', () => ({
  api: {
    get: (p: string) => (p === '/taste/feedback' ? getTasteFeedback() : Promise.reject(new Error(p))),
    del: (p: string) => del(p),
  },
}))

import { RecommendationsSection } from './RecommendationsSection'

function renderIt() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <RecommendationsSection />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  del.mockResolvedValue(null)
})

describe('RecommendationsSection', () => {
  it('shows an empty state when nothing is hidden', async () => {
    getTasteFeedback.mockResolvedValue([])
    renderIt()
    expect(await screen.findByText('Nothing hidden')).toBeInTheDocument()
  })

  it('lists hidden artists and songs and brings one back', async () => {
    getTasteFeedback.mockResolvedValue([
      { targetKind: 'artist', targetKey: 'blur', label: 'Blur', createdAt: 2 },
      { targetKind: 'track', targetKey: 'trk_1', label: 'Hurt', sublabel: 'Johnny Cash', createdAt: 1 },
    ])
    renderIt()
    expect(await screen.findByRole('region', { name: 'Hidden artists' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Hidden songs' })).toBeInTheDocument()
    expect(screen.getByText('Johnny Cash')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show Blur again' }))
    await waitFor(() => expect(del).toHaveBeenCalledWith('/taste/feedback/artist/blur'))
  })
})
