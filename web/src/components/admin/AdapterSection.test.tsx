import { render, screen, fireEvent } from '@testing-library/react'
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
})
