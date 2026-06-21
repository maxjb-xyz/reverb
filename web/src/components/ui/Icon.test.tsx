import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Icon } from './Icon'

describe('Icon', () => {
  it('renders an svg using currentColor (themable) for a known name', () => {
    const { container } = render(<Icon name="play" />)
    const svg = container.querySelector('svg')
    expect(svg).toBeTruthy()
    expect(svg!.getAttribute('viewBox')).toBe('0 0 24 24')
  })
  it('is aria-hidden by default and labelled when a label is given', () => {
    const { container, rerender } = render(<Icon name="search" />)
    expect(container.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true')
    rerender(<Icon name="search" aria-label="Search" />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-hidden')).toBeNull()
    expect(svg.getAttribute('aria-label')).toBe('Search')
    expect(svg.getAttribute('role')).toBe('img')
  })
})
