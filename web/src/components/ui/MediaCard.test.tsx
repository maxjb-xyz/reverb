import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MediaCard } from './MediaCard'

describe('MediaCard', () => {
  it('renders the title', () => {
    render(<MediaCard title="OK Computer" />)
    expect(screen.getByText('OK Computer')).toBeInTheDocument()
  })

  it('renders the subtitle when provided', () => {
    render(<MediaCard title="OK Computer" subtitle="Radiohead · 1997" />)
    expect(screen.getByText('Radiohead · 1997')).toBeInTheDocument()
  })

  it('does not render subtitle element when omitted', () => {
    const { container } = render(<MediaCard title="OK Computer" />)
    expect(container.querySelector('[data-testid="mediacard-subtitle"]')).not.toBeInTheDocument()
  })

  it('renders a Cover (img or placeholder) when coverId is provided', () => {
    const { container } = render(<MediaCard title="OK Computer" coverId="art-123" />)
    // Cover renders either an img or a placeholder — either way the wrapper exists
    const cover = container.querySelector('[data-testid="mediacard-cover"]')
    expect(cover).toBeInTheDocument()
  })

  it('fires onPlay when the play button is clicked (not onClick)', () => {
    const onPlay = vi.fn()
    const onClick = vi.fn()
    render(<MediaCard title="OK Computer" onPlay={onPlay} onClick={onClick} />)
    const playBtn = screen.getByRole('button', { name: /play/i })
    fireEvent.click(playBtn)
    expect(onPlay).toHaveBeenCalledTimes(1)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('fires onClick when the card body is clicked', () => {
    const onClick = vi.fn()
    render(<MediaCard title="OK Computer" onClick={onClick} />)
    const card = screen.getByRole('button', { name: /OK Computer/i })
    fireEvent.click(card)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders the badge slot when provided', () => {
    render(<MediaCard title="OK Computer" badge={<span>Downloaded</span>} />)
    expect(screen.getByText('Downloaded')).toBeInTheDocument()
  })

  it('applies rounded-full class when rounded="full"', () => {
    const { container } = render(<MediaCard title="Artist" rounded="full" />)
    // The Cover inside should have rounded-full
    const cover = container.querySelector('[data-testid="mediacard-cover"]')
    expect(cover?.className).toMatch(/rounded-full/)
  })

  it('has bg-raised on the card root', () => {
    const { container } = render(<MediaCard title="Test" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/bg-raised/)
  })

  it('play button has focus-visible ring', () => {
    render(<MediaCard title="OK Computer" onPlay={vi.fn()} />)
    const playBtn = screen.getByRole('button', { name: /play/i })
    expect(playBtn.className).toMatch(/focus-visible:ring/)
  })
})
