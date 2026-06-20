import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Album from './Album'

describe('Album page', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            id: 'al1', name: 'Great Album', artist: 'A', artistId: 'ar1', year: 2021,
            tracks: [{ id: 't1', title: 'Track One', artist: 'A', durationMs: 1000, trackNumber: 1 }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders album header and tracks', async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={['/album/al1']}>
          <Routes>
            <Route path="/album/:id" element={<Album />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
    await waitFor(() => expect(screen.getByText('Great Album')).toBeInTheDocument())
    expect(screen.getByText('Track One')).toBeInTheDocument()
  })
})
