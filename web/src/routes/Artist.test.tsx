import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import Artist from './Artist'

// ---------------------------------------------------------------------------
// Module mocks — must be hoisted before any imports that pull these modules.
// ---------------------------------------------------------------------------

vi.mock('../lib/coverageApi', () => ({
  useArtistDetail: vi.fn(),
}))

vi.mock('../lib/coverageStore', () => ({
  useCoverageStream: vi.fn(),
}))

vi.mock('../lib/downloadApi', () => ({
  postBatchDownload: vi.fn().mockResolvedValue([]),
}))

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: vi.fn().mockReturnValue({ source: 'spotify', id: 'ar-radiohead' }),
    useNavigate: vi.fn().mockReturnValue(vi.fn()),
  }
})

import { useArtistDetail } from '../lib/coverageApi'
import { useCoverageStream } from '../lib/coverageStore'
import { postBatchDownload } from '../lib/downloadApi'

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const STUB_DETAIL = {
  source: 'spotify',
  id: 'ar-radiohead',
  name: 'Radiohead',
  resolved: true,
  coverUrl: 'https://cdn.example.com/radiohead.jpg',
  albums: [
    {
      source: 'spotify',
      externalId: 'AL',
      name: 'Kid A',
      year: 2000,
      kind: 'album' as const,
      totalTracks: 10,
      coverUrl: 'https://cdn.example.com/kida.jpg',
    },
    {
      source: 'spotify',
      externalId: 'S1',
      name: 'Creep',
      year: 1992,
      kind: 'single' as const,
      totalTracks: 1,
      coverUrl: 'https://cdn.example.com/creep.jpg',
    },
  ],
}

const MISSING_TRACK = {
  source: 'spotify',
  externalId: 'm1',
  title: 'x',
  durationMs: 1000,
}

const STUB_COVERAGE = {
  AL: {
    source: 'spotify',
    externalAlbumId: 'AL',
    state: 'partial' as const,
    ownedCount: 7,
    totalCount: 10,
    missingTracks: [MISSING_TRACK],
  },
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function wrapper(ui: React.ReactElement) {
  return render(
    <MemoryRouter initialEntries={['/artist/spotify/ar-radiohead']}>
      <Routes>
        <Route path="/artist/:source/:id" element={ui} />
        <Route path="/album/:source/:id" element={<div data-testid="album-page" />} />
      </Routes>
    </MemoryRouter>,
  )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('Artist page', () => {
  beforeEach(() => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: STUB_DETAIL,
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)

    vi.mocked(useCoverageStream).mockReturnValue(STUB_COVERAGE)

    vi.mocked(postBatchDownload).mockClear()
  })

  it('renders loading skeleton while fetching', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.getByTestId('artist-skeleton')).toBeInTheDocument()
  })

  it('renders both album cards', () => {
    wrapper(<Artist />)
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Creep' })).toBeInTheDocument()
  })

  it('renders the partial coverage chip with 7/10 for Kid A', () => {
    wrapper(<Artist />)
    expect(screen.getByText('7/10')).toBeInTheDocument()
  })

  it('clicking "Singles & EPs" filter hides Kid A and shows Creep', () => {
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Singles & EPs' }))
    expect(screen.queryByRole('button', { name: 'Kid A' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Creep' })).toBeInTheDocument()
  })

  it('clicking "Albums" filter shows Kid A and hides Creep', () => {
    wrapper(<Artist />)
    fireEvent.click(screen.getByRole('button', { name: 'Albums' }))
    expect(screen.getByRole('button', { name: 'Kid A' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Creep' })).not.toBeInTheDocument()
  })

  it('"Download all missing" button calls postBatchDownload with missing tracks', () => {
    wrapper(<Artist />)
    const dlBtn = screen.getByRole('button', { name: /download all missing/i })
    fireEvent.click(dlBtn)
    expect(postBatchDownload).toHaveBeenCalledWith([MISSING_TRACK])
  })

  it('shows EmptyState when artist not found', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.getByText(/artist not found/i)).toBeInTheDocument()
  })

  it('does not render "Download all missing" in degrade mode (resolved=false)', () => {
    vi.mocked(useArtistDetail).mockReturnValue({
      data: { ...STUB_DETAIL, resolved: false },
      isLoading: false,
      isError: false,
    } as ReturnType<typeof useArtistDetail>)
    vi.mocked(useCoverageStream).mockReturnValue({})
    wrapper(<Artist />)
    expect(screen.queryByRole('button', { name: /download all missing/i })).not.toBeInTheDocument()
  })
})
