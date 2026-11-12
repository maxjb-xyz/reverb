import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

const getTasteFeedback = vi.fn()
const getIndex = vi.fn()
const getAnalysis = vi.fn()
const del = vi.fn()
const post = vi.fn()
const put = vi.fn()
const getEngineSettings = vi.fn()
vi.mock('../../lib/api', () => ({
  api: {
    get: (p: string) =>
      p === '/taste/feedback'
        ? getTasteFeedback()
        : p === '/engine/index'
          ? getIndex()
          : p === '/engine/analysis'
            ? getAnalysis()
            : p === '/engine/settings'
              ? getEngineSettings()
              : Promise.reject(new Error(p)),
    del: (p: string) => del(p),
    post: (p: string) => post(p),
    put: (p: string, b?: unknown) => put(p, b),
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
  getAnalysis.mockResolvedValue({ enabled: false, running: false, analyzed: 0, failed: 0, total: 0 })
  getEngineSettings.mockResolvedValue({ deezer: true })
  put.mockImplementation((_p: string, b: unknown) => Promise.resolve(b))
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

  it('names where similar artists come from', async () => {
    caps = ['manage_server']
    getTasteFeedback.mockResolvedValue([])
    getIndex.mockResolvedValue({ running: false, tracks: 95, artists: 12, artistsWithSimilar: 10, lastIndexedAt: 1, similarSources: ['deezer'] })
    renderIt()
    expect(await screen.findByText(/Similar artists found for 10 of them, from Deezer\./)).toBeInTheDocument()
  })

  it('explains when nothing can find similar artists and lets managers switch Deezer back on', async () => {
    caps = ['manage_server']
    getTasteFeedback.mockResolvedValue([])
    getIndex.mockResolvedValue({ running: false, tracks: 95, artists: 12, artistsWithSimilar: 0, lastIndexedAt: 1, similarSources: [] })
    getEngineSettings.mockResolvedValue({ deezer: false })
    renderIt()
    expect(await screen.findByText(/Nothing can look up similar artists/)).toBeInTheDocument()
    const row = await screen.findByTestId('engine-deezer')
    expect(row).toHaveTextContent('Use Deezer for recommendations')
    fireEvent.click(within(row).getByRole('tab', { name: 'On' }))
    await waitFor(() => expect(put).toHaveBeenCalledWith('/engine/settings', { deezer: true }))
  })

  it('shows managers audio analysis progress once an analyzer is set up', async () => {
    caps = ['manage_server']
    getTasteFeedback.mockResolvedValue([])
    getIndex.mockResolvedValue({ running: false, tracks: 10, artists: 2, artistsWithSimilar: 1, lastIndexedAt: 1 })
    getAnalysis.mockResolvedValue({ enabled: true, running: false, model: 'clap', analyzed: 30, failed: 2, total: 100, lastError: 'could not decode' })
    post.mockResolvedValue(null)
    renderIt()
    const row = await screen.findByTestId('analysis-status')
    expect(row).toHaveTextContent("30 of 100 songs analyzed. 2 couldn't be read")
    expect(row).toHaveTextContent('once 50 are done')
    expect(row).toHaveTextContent('Last problem: could not decode')
    fireEvent.click(screen.getByRole('button', { name: 'Continue now' }))
    await waitFor(() => expect(post).toHaveBeenCalledWith('/engine/analysis/run'))
  })

  it('hides audio analysis when no analyzer is set up', async () => {
    caps = ['manage_server']
    getTasteFeedback.mockResolvedValue([])
    getIndex.mockResolvedValue({ running: false, tracks: 10, artists: 2, artistsWithSimilar: 1, lastIndexedAt: 1 })
    renderIt()
    await screen.findByText(/10 songs by 2 artists/)
    await waitFor(() => expect(getAnalysis).toHaveBeenCalled())
    expect(screen.queryByTestId('analysis-status')).toBeNull()
  })

  it('hides the index status from non-managers', async () => {
    getTasteFeedback.mockResolvedValue([])
    renderIt()
    await screen.findByText('Nothing hidden')
    expect(screen.queryByText('Recommendation index')).not.toBeInTheDocument()
    expect(getIndex).not.toHaveBeenCalled()
  })
})
