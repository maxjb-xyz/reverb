import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Artist from './Artist'
import { makeAlbum } from '../test/factories'

const stubAlbum1 = makeAlbum({ id: 'al1', name: 'First Album', year: 2019, coverArtId: '' })
const stubAlbum2 = makeAlbum({ id: 'al2', name: 'Second Album', year: 2022, coverArtId: '' })

const stubArtist = {
  id: 'ar1',
  name: 'Great Artist',
  coverArtId: '',
  albumCount: 2,
  albums: [stubAlbum1, stubAlbum2],
}

function wrapper(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/artist/ar1']}>
        <Routes>
          <Route path="/artist/:id" element={ui} />
          <Route path="/album/:id" element={<div data-testid="album-page" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Artist page', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(stubArtist), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders loading skeleton while fetching', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    wrapper(<Artist />)
    expect(screen.getByTestId('artist-skeleton')).toBeInTheDocument()
  })

  it('renders artist name after load', async () => {
    wrapper(<Artist />)
    await waitFor(() => expect(screen.getByText('Great Artist')).toBeInTheDocument())
  })

  it('renders library + album count subtitle', async () => {
    wrapper(<Artist />)
    await waitFor(() => expect(screen.getByText(/in your library/i)).toBeInTheDocument())
    expect(screen.getByText(/2 albums/i)).toBeInTheDocument()
  })

  it('renders album cards for each album', async () => {
    wrapper(<Artist />)
    await waitFor(() => expect(screen.getByText('First Album')).toBeInTheDocument())
    expect(screen.getByText('Second Album')).toBeInTheDocument()
  })

  it('clicking an album card navigates to /album/:id', async () => {
    wrapper(<Artist />)
    await waitFor(() => expect(screen.getByText('First Album')).toBeInTheDocument())
    // MediaCard renders as a button with aria-label = title
    fireEvent.click(screen.getByRole('button', { name: 'First Album' }))
    await waitFor(() => expect(screen.getByTestId('album-page')).toBeInTheDocument())
  })

  it('shows EmptyState when artist not found', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('null', { status: 404, headers: { 'Content-Type': 'application/json' } })),
    )
    wrapper(<Artist />)
    await waitFor(() => expect(screen.getByText(/artist not found/i)).toBeInTheDocument())
  })
})
