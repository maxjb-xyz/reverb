import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PageHeader } from './PageHeader'

describe('PageHeader', () => {
  it('renders the title as the page h1 with subtitle and actions', () => {
    render(<PageHeader title="Downloads" subtitle="2 queued" actions={<button>Clear</button>} />)
    expect(screen.getByRole('heading', { level: 1, name: 'Downloads' })).toBeInTheDocument()
    expect(screen.getByText('2 queued')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument()
  })

  it('omits subtitle and actions when not given', () => {
    const { container } = render(<PageHeader title="Stats" />)
    expect(container.querySelector('p')).toBeNull()
    expect(container.querySelectorAll('header > div')).toHaveLength(1)
  })
})
