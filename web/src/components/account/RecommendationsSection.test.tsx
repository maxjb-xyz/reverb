import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const getTasteFeedback = vi.fn()
const getIndex = vi.fn()
const del = vi.fn()
const post = vi.fn()
vi.mock('../../lib/api', () => ({
  api: {
    get: (p: string) =>
      p === '/taste/feedback' ? getTasteFeedback() : p === '/engine/index' ? getIndex() : Promise.reject(new Error(p)),
    del: (p: string) => del(p),
    post: (p: string) => post(p),
  },
}))
let caps: string[] = []
vi.mock('../../lib/authStore', () => ({
  useAuthStore: (sel: (s: { me: { capabilities: string[] } }) => unknown) => sel({ me: { capabilities: caps } }),
  isManagerCaps: (c: string[] | undefined) => !!c?.includes('manage_server'),
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
  caps = []
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

  it('shows managers the index status and lets them rebuild it', async () => {
    caps = ['manage_server']
    getTasteFeedback.mockResolvedValue([])
    getIndex.mockResolvedValue({ running: false, tracks: 1234, artists: 56, artistsWithSimilar: 40, lastIndexedAt: 1 })
    post.mockResolvedValue(null)
    renderIt()
    expect(await screen.findByText(/1,234 songs by 56 artists\. Similar artists found for 40/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Rebuild now' }))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/engine/index/rebuild'))
  })

  it('hides the index status from non-managers', async () => {
    getTasteFeedback.mockResolvedValue([])
    renderIt()
    await screen.findByText('Nothing hidden')
    expect(screen.queryByText('Recommendation index')).not.toBeInTheDocument()
    expect(getIndex).not.toHaveBeenCalled()
  })
})
