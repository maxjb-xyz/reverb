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
  const cardHandlers = {
    onTest: vi.fn(),
    onEdit: vi.fn(),
    onToggle: vi.fn(),
    onRemove: vi.fn(),
  }
  const onAdd = vi.fn()

  beforeEach(() => {
    Object.values(cardHandlers).forEach((fn) => fn.mockClear())
    onAdd.mockClear()
  })

  it('renders the section title', () => {
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={[]}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    expect(screen.getByText('Search providers')).toBeInTheDocument()
  })

  it('shows EmptyState when no instances', () => {
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={[]}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    // EmptyState renders with a title
    expect(screen.getByText(/no search providers/i)).toBeInTheDocument()
  })

  it('renders one AdapterCard per instance', () => {
    const instances = [
      makeInstance({ id: 'a', name: 'Spotify' }),
      makeInstance({ id: 'b', name: 'MusicBrainz' }),
    ]
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={instances}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    expect(screen.getByText('Spotify')).toBeInTheDocument()
    expect(screen.getByText('MusicBrainz')).toBeInTheDocument()
  })

  it('calls onAdd when Add button is clicked', () => {
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={[]}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    fireEvent.click(screen.getByRole('button', { name: /add search/i }))
    expect(onAdd).toHaveBeenCalledTimes(1)
  })

  it('renders the instance count in the header', () => {
    const instances = [makeInstance({ id: 'a' }), makeInstance({ id: 'b' })]
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={instances}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    // The count badge in the section header shows the number of instances
    expect(screen.getAllByText('2').length).toBeGreaterThanOrEqual(1)
  })

  it('orders instances by priority', () => {
    const instances = [
      makeInstance({ id: 'b', name: 'Second', priority: 2 }),
      makeInstance({ id: 'a', name: 'First', priority: 1 }),
    ]
    render(
      <AdapterSection
        title="Search providers"
        type="search"
        instances={instances}
        available={[makeAvailable()]}
        onAdd={onAdd}
        {...cardHandlers}
      />
    )
    const cards = screen.getAllByRole('article')
    expect(cards[0]).toHaveTextContent('First')
    expect(cards[1]).toHaveTextContent('Second')
  })
})
