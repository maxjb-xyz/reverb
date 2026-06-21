import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Badge } from './Badge'

describe('Badge', () => {
  it('renders children', () => {
    render(<Badge kind="in-library">In Library</Badge>)
    expect(screen.getByText('In Library')).toBeInTheDocument()
  })

  it('applies accent color for in-library', () => {
    const { container } = render(<Badge kind="in-library">In Library</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-accent/)
  })

  it('applies accent color for downloaded', () => {
    const { container } = render(<Badge kind="downloaded">Downloaded</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-accent/)
  })

  it('applies border styling for available', () => {
    const { container } = render(<Badge kind="available">Available</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/border/)
  })

  it('applies muted color for disabled', () => {
    const { container } = render(<Badge kind="disabled">Disabled</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-text-muted/)
  })

  it('applies accent color for downloading', () => {
    const { container } = render(<Badge kind="downloading">Downloading</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-accent/)
  })

  it('renders a dot for status kind', () => {
    const { container } = render(<Badge kind="status" tone="success">Connected</Badge>)
    const dot = container.querySelector('[data-testid="status-dot"]')
    expect(dot).toBeInTheDocument()
  })

  it('applies success color for status with tone=success', () => {
    const { container } = render(<Badge kind="status" tone="success">OK</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-success/)
  })

  it('applies warning color for status with tone=warning', () => {
    const { container } = render(<Badge kind="status" tone="warning">Warn</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-warning/)
  })

  it('applies error color for status with tone=error', () => {
    const { container } = render(<Badge kind="status" tone="error">Error</Badge>)
    const el = container.firstChild as HTMLElement
    expect(el.className).toMatch(/text-error/)
  })
})
