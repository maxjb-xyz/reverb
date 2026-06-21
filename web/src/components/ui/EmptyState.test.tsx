import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { EmptyState } from './EmptyState'

describe('EmptyState', () => {
  it('renders the title', () => {
    render(<EmptyState icon="search" title="No results" />)
    expect(screen.getByText('No results')).toBeInTheDocument()
  })

  it('renders the hint when provided', () => {
    render(<EmptyState icon="search" title="No results" hint="Try a different query" />)
    expect(screen.getByText('Try a different query')).toBeInTheDocument()
  })

  it('does not render a hint element when hint is absent', () => {
    const { container } = render(<EmptyState icon="search" title="Empty" />)
    // hint paragraph should not be present
    expect(container.querySelector('[data-testid="empty-hint"]')).not.toBeInTheDocument()
  })

  it('renders the action when provided', () => {
    render(
      <EmptyState icon="search" title="Empty" action={<button>Refresh</button>} />
    )
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument()
  })

  it('renders the icon element', () => {
    const { container } = render(<EmptyState icon="search" title="Empty" />)
    const iconWrapper = container.querySelector('[data-testid="empty-icon"]')
    expect(iconWrapper).toBeInTheDocument()
  })
})
