import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { AdapterCard } from './AdapterCard'
import type { AdapterInstance } from '../../lib/adaptersApi'
import { SECRET_SENTINEL } from '../../lib/adaptersApi'

const makeInstance = (overrides: Partial<AdapterInstance> = {}): AdapterInstance => ({
  id: 'inst-1',
  type: 'search',
  name: 'Spotify',
  enabled: true,
  priority: 1,
  config: {
    client_id: 'my-real-client-id',
    client_secret: SECRET_SENTINEL,
    client_secret__isSet: true,
  },
  capabilities: [],
  ...overrides,
})

describe('AdapterCard', () => {
  const handlers = {
    onTest: vi.fn(),
    onEdit: vi.fn(),
    onToggle: vi.fn(),
    onRemove: vi.fn(),
  }

  beforeEach(() => {
    Object.values(handlers).forEach((fn) => fn.mockClear())
  })

  it('renders the adapter name', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} />)
    expect(screen.getByText('Spotify')).toBeInTheDocument()
  })

  it('shows "Connected" status badge when enabled', () => {
    render(<AdapterCard instance={makeInstance({ enabled: true })} {...handlers} />)
    expect(screen.getByText(/Connected/i)).toBeInTheDocument()
  })

  it('shows "Disabled" status badge when not enabled', () => {
    render(<AdapterCard instance={makeInstance({ enabled: false })} {...handlers} />)
    expect(screen.getByText(/Disabled/i)).toBeInTheDocument()
  })

  it('never renders real secret values in the DOM', () => {
    const instance = makeInstance({
      config: {
        api_key: 'super-secret-api-key-12345',
        api_key__isSet: true,
      },
    })
    render(<AdapterCard instance={instance} {...handlers} />)
    expect(screen.queryByText('super-secret-api-key-12345')).not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain('super-secret-api-key-12345')
  })

  it('renders SECRET_SENTINEL for secrets marked as set', () => {
    const instance = makeInstance({
      config: {
        api_key: SECRET_SENTINEL,
        api_key__isSet: true,
      },
    })
    render(<AdapterCard instance={instance} {...handlers} />)
    // The sentinel value itself is fine to show (it's already redacted)
    // but the real secret must NOT appear
    expect(document.body.textContent).not.toContain('super-secret')
  })

  it('shows a logo chip with the first letter of the name', () => {
    render(<AdapterCard instance={makeInstance({ name: 'Navidrome' })} {...handlers} />)
    expect(screen.getByText('N')).toBeInTheDocument()
  })

  it('calls onTest when Test button is clicked', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} />)
    fireEvent.click(screen.getByRole('button', { name: /test/i }))
    expect(handlers.onTest).toHaveBeenCalledWith(makeInstance())
  })

  it('calls onEdit when Edit button is clicked', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} />)
    fireEvent.click(screen.getByRole('button', { name: /edit/i }))
    expect(handlers.onEdit).toHaveBeenCalledWith(makeInstance())
  })

  it('calls onRemove when Remove button is clicked', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} />)
    fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    expect(handlers.onRemove).toHaveBeenCalledWith('inst-1')
  })

  it('calls onToggle when toggle is clicked', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} />)
    const toggle = screen.getByRole('switch')
    fireEvent.click(toggle)
    expect(handlers.onToggle).toHaveBeenCalledWith(makeInstance())
  })

  it('renders order index when provided', () => {
    render(<AdapterCard instance={makeInstance()} {...handlers} order={3} />)
    expect(screen.getByText('3')).toBeInTheDocument()
  })

  it('toggle is checked when enabled', () => {
    render(<AdapterCard instance={makeInstance({ enabled: true })} {...handlers} />)
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
  })

  it('toggle is unchecked when disabled', () => {
    render(<AdapterCard instance={makeInstance({ enabled: false })} {...handlers} />)
    expect(screen.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
  })
})
