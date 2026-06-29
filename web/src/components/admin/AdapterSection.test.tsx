import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { AdapterSection } from './AdapterSection'
import type { AdapterInstance, AvailableAdapter } from '../../lib/adaptersApi'

const makeInstance = (overrides: Partial<AdapterInstance> = {}): AdapterInstance => ({
  id: 'inst-1',
  type: 'search',
  name: 'Spotify',
  enabled: true,
  priority: 1,
  config: {},
  capabilities: [],
  ...overrides,
})

const makeAvailable = (overrides: Partial<AvailableAdapter> = {}): AvailableAdapter => ({
  type: 'search',
  name: 'Spotify',
  configSchema: { fields: [] },
  capabilities: [],
  ...overrides,
})

describe('AdapterSection', () => {
  const handlers = {
    onCreate: vi.fn().mockResolvedValue(undefined),
    onUpdate: vi.fn().mockResolvedValue(undefined),
    onToggle: vi.fn(),
    onRemove: vi.fn(),
  }

  beforeEach(() => {
    Object.values(handlers).forEach((fn) => fn.mockClear())
  })

  it('renders the section title', () => {
    render(<AdapterSection title="Search providers" type="search" instances={[]} available={[makeAvailable()]} {...handlers} />)
    expect(screen.getByText('Search providers')).toBeInTheDocument()
  })

  it('shows EmptyState when no instances', () => {
    render(<AdapterSection title="Search providers" type="search" instances={[]} available={[makeAvailable()]} {...handlers} />)
    expect(screen.getByText(/no search providers/i)).toBeInTheDocument()
  })

  it('renders one AdapterCard per instance', () => {
    const instances = [makeInstance({ id: 'a', name: 'Spotify' }), makeInstance({ id: 'b', name: 'MusicBrainz' })]
    render(<AdapterSection title="Search providers" type="search" instances={instances} available={[makeAvailable()]} {...handlers} />)
    expect(screen.getByText('Spotify')).toBeInTheDocument()
    expect(screen.getByText('MusicBrainz')).toBeInTheDocument()
  })

  it('opens the inline add form when the Add button is clicked (single provider auto-selected)', () => {
    render(<AdapterSection title="Search providers" type="search" instances={[]} available={[makeAvailable()]} {...handlers} />)
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    // With one available provider it is auto-selected straight to its config form.
    expect(screen.getByText('Add Spotify')).toBeInTheDocument()
  })

  it('shows a provider chooser when more than one provider is available', () => {
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={[]}
        available={[makeAvailable({ name: 'Spotify' }), makeAvailable({ name: 'Deezer' })]}
        {...handlers}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    expect(screen.getByText(/choose a search provider/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Spotify' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Deezer' })).toBeInTheDocument()
  })

  it('renders the instance count in the header', () => {
    const instances = [makeInstance({ id: 'a' }), makeInstance({ id: 'b' })]
    render(<AdapterSection title="Search providers" type="search" instances={instances} available={[makeAvailable()]} {...handlers} />)
    expect(screen.getAllByText('2').length).toBeGreaterThanOrEqual(1)
  })

  it('orders instances by priority', () => {
    const instances = [
      makeInstance({ id: 'b', name: 'Second', priority: 2 }),
      makeInstance({ id: 'a', name: 'First', priority: 1 }),
    ]
    render(<AdapterSection title="Search providers" type="search" instances={instances} available={[makeAvailable()]} {...handlers} />)
    const cards = screen.getAllByRole('article')
    expect(cards[0]).toHaveTextContent('First')
    expect(cards[1]).toHaveTextContent('Second')
  })

  it('search adapter rows render priority reorder arrows', () => {
    const onReorder = vi.fn()
    const instances = [makeInstance({ id: 'a', name: 'Spotify', type: 'search' })]
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={instances}
        available={[makeAvailable()]}
        onReorder={onReorder}
        {...handlers}
      />,
    )
    expect(screen.getByRole('button', { name: /move spotify up/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /move spotify down/i })).toBeInTheDocument()
  })

  it('downloader adapter rows do NOT render priority reorder arrows', () => {
    const onReorder = vi.fn()
    const instances = [makeInstance({ id: 'dl-1', name: 'spotdl', type: 'downloader' })]
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={instances}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' })]}
        onReorder={onReorder}
        {...handlers}
      />,
    )
    expect(screen.queryByRole('button', { name: /move spotdl up/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /move spotdl down/i })).not.toBeInTheDocument()
  })

  it('downloader section does NOT show the "Order in Settings" hint (ordering is now here in Admin)', () => {
    const instances = [makeInstance({ id: 'dl-1', name: 'spotdl', type: 'downloader' })]
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={instances}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' })]}
        onMoveInColumn={vi.fn()}
        {...handlers}
      />,
    )
    expect(screen.queryByText(/order in settings/i)).not.toBeInTheDocument()
  })
})

// spotDL: track+album (granularities {track:0,album:0}); Lidarr: album only (granularities {album:1})
const dlSpotdl = makeInstance({
  id: 'dl-1',
  type: 'downloader',
  name: 'spotdl',
  granularities: { track: 0, album: 0 },
  supportedGranularities: ['track', 'album'],
})
const dlLidarr = makeInstance({
  id: 'dl-2',
  type: 'downloader',
  name: 'lidarr',
  granularities: { album: 1 },
  supportedGranularities: ['album'],
})

describe('AdapterSection downloader — two-column ordering (moved from Settings)', () => {
  const onMoveInColumn = vi.fn().mockResolvedValue(undefined)
  const handlers = {
    onCreate: vi.fn().mockResolvedValue(undefined),
    onUpdate: vi.fn().mockResolvedValue(undefined),
    onToggle: vi.fn(),
    onRemove: vi.fn(),
  }

  beforeEach(() => {
    onMoveInColumn.mockClear()
    Object.values(handlers).forEach((fn) => fn.mockClear())
  })

  it('renders a Song column with only spotdl (has track granularity)', () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const songCol = screen.getByTestId('downloaders-song-col')
    expect(within(songCol).getByText('spotdl')).toBeInTheDocument()
    expect(within(songCol).queryByText('lidarr')).toBeNull()
  })

  it('renders an Album column with spotdl first then lidarr (sorted by album granularity order)', () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const albumCol = screen.getByTestId('downloaders-album-col')
    const names = within(albumCol).getAllByTestId('downloader-name').map((el) => el.textContent)
    expect(names).toEqual(['spotdl', 'lidarr'])
  })

  it('first-row up button is disabled in Song column', () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const songCol = screen.getByTestId('downloaders-song-col')
    const upButtons = within(songCol).getAllByRole('button', { name: /move up/i })
    expect(upButtons[0]).toBeDisabled()
  })

  it('last-row down button is disabled in Album column', () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const albumCol = screen.getByTestId('downloaders-album-col')
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    expect(downButtons[downButtons.length - 1]).toBeDisabled()
  })

  it('clicking down on spotdl in Album column calls onMoveInColumn with album column + index 0 + down + "album"', async () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const albumCol = screen.getByTestId('downloaders-album-col')
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    fireEvent.click(downButtons[0])
    await waitFor(() => expect(onMoveInColumn).toHaveBeenCalledTimes(1))
    // Called with (column, 0, 'down', 'album')
    const [colArg, idxArg, dirArg, gArg] = onMoveInColumn.mock.calls[0] as [unknown[], number, string, string]
    expect(idxArg).toBe(0)
    expect(dirArg).toBe('down')
    expect(gArg).toBe('album')
    // The column passed must be the album-sorted array: spotdl then lidarr
    expect(Array.isArray(colArg)).toBe(true)
    const colNames = (colArg as Array<{ name: string }>).map((a) => a.name)
    expect(colNames).toEqual(['spotdl', 'lidarr'])
  })

  it('reordering Album column does NOT affect Song column (independence)', async () => {
    render(
      <AdapterSection
        title="Downloaders"
        type="downloader"
        instances={[dlSpotdl, dlLidarr]}
        available={[makeAvailable({ type: 'downloader', name: 'spotdl' }), makeAvailable({ type: 'downloader', name: 'lidarr' })]}
        onMoveInColumn={onMoveInColumn}
        {...handlers}
      />,
    )
    const albumCol = screen.getByTestId('downloaders-album-col')
    const downButtons = within(albumCol).getAllByRole('button', { name: /move down/i })
    fireEvent.click(downButtons[0])
    await waitFor(() => expect(onMoveInColumn).toHaveBeenCalledTimes(1))
    // The Song column must still render with spotdl only
    const songCol = screen.getByTestId('downloaders-song-col')
    expect(within(songCol).getByText('spotdl')).toBeInTheDocument()
    expect(within(songCol).queryByText('lidarr')).toBeNull()
  })
})
