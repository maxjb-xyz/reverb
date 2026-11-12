import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Cover } from './Cover'
import { initialsOf, looksLikeBlankPlaceholder } from '../../lib/avatar'

describe('Cover', () => {
  it('renders an img with loading=lazy when src is given', () => {
    render(<Cover src="https://example.com/art.jpg" alt="OK Computer" />)
    const img = screen.getByRole('img', { name: 'OK Computer' })
    expect(img).toBeInTheDocument()
    expect(img).toHaveAttribute('loading', 'lazy')
  })

  it('renders img with correct alt', () => {
    render(<Cover src="https://example.com/art.jpg" alt="OK Computer" />)
    const img = screen.getByAltText('OK Computer')
    expect(img).toBeInTheDocument()
  })

  it('shows a skeleton initially (before load event)', () => {
    const { container } = render(<Cover src="https://example.com/art.jpg" alt="Art" />)
    // Skeleton placeholder should be visible before image fires onLoad
    const skeleton = container.querySelector('[data-testid="cover-skeleton"]')
    expect(skeleton).toBeInTheDocument()
  })

  it('hides skeleton after image load', () => {
    const { container } = render(<Cover src="https://example.com/art.jpg" alt="Art" />)
    const img = container.querySelector('img')!
    fireEvent.load(img)
    const skeleton = container.querySelector('[data-testid="cover-skeleton"]')
    expect(skeleton).not.toBeInTheDocument()
  })

  it('renders a placeholder Icon when src is missing', () => {
    const { container } = render(<Cover alt="No art" />)
    // should show a bg-raised placeholder container, no img
    const img = container.querySelector('img')
    expect(img).not.toBeInTheDocument()
    const placeholder = container.querySelector('[data-testid="cover-placeholder"]')
    expect(placeholder).toBeInTheDocument()
  })

  it('renders placeholder on img error', () => {
    const { container } = render(<Cover src="https://bad.url/img.jpg" alt="Bad" />)
    const img = container.querySelector('img')!
    fireEvent.error(img)
    const placeholder = container.querySelector('[data-testid="cover-placeholder"]')
    expect(placeholder).toBeInTheDocument()
  })

  it('falls back to fallbackSrc on first error, placeholder on second', () => {
    const { container } = render(
      <Cover src="https://bad/song.jpg" fallbackSrc="https://ok/album.jpg" alt="X" />,
    )
    const img = container.querySelector('img')!
    expect(img).toHaveAttribute('src', 'https://bad/song.jpg')
    // First error → swap to the fallback (album) src, NOT a placeholder yet.
    fireEvent.error(img)
    const img2 = container.querySelector('img')!
    expect(img2).toHaveAttribute('src', 'https://ok/album.jpg')
    expect(container.querySelector('[data-testid="cover-placeholder"]')).not.toBeInTheDocument()
    // Second error (fallback also failed) → placeholder.
    fireEvent.error(img2)
    expect(container.querySelector('[data-testid="cover-placeholder"]')).toBeInTheDocument()
  })

  it('applies rounded-full when specified', () => {
    const { container } = render(<Cover alt="Artist" rounded="full" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/rounded-full/)
  })

  it('applies rounded-md by default', () => {
    const { container } = render(<Cover alt="Art" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/rounded-md/)
  })

  it('applies custom size as inline style when size is a number', () => {
    const { container } = render(<Cover alt="Art" size={80} />)
    const root = container.firstChild as HTMLElement
    expect(root.getAttribute('style')).toMatch(/80px/)
  })

  it('applies w-full h-full when size is "full"', () => {
    const { container } = render(<Cover alt="Art" size="full" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/w-full/)
    expect(root.className).toMatch(/h-full/)
  })

  it('round (artist) covers fall back to initials instead of the music icon', () => {
    render(<Cover alt="Halcyon Drift" rounded="full" />)
    expect(screen.getByTestId('cover-placeholder').textContent).toBe('HD')
  })

  it('square covers keep the music icon placeholder', () => {
    render(<Cover alt="Halcyon Drift" />)
    expect(screen.getByTestId('cover-placeholder').textContent).toBe('')
  })
})

describe('initialsOf', () => {
  it('takes the first letters of the first and last word', () => {
    expect(initialsOf('Halcyon Drift')).toBe('HD')
    expect(initialsOf('ironwood')).toBe('I')
    expect(initialsOf('The Very Long Band Name')).toBe('TN')
    expect(initialsOf('  ')).toBe('')
  })
})

describe('looksLikeBlankPlaceholder', () => {
  const fill = (rgb: [number, number, number], n = 256) => {
    const d = new Uint8ClampedArray(n * 4)
    for (let i = 0; i < n; i++) d.set([...rgb, 255], i * 4)
    return d
  }

  it('flags an almost entirely white, colourless image', () => {
    const d = fill([250, 250, 250])
    d.set([120, 120, 120, 255], 0) // a little grey silhouette
    expect(looksLikeBlankPlaceholder(d)).toBe(true)
  })

  it('does not flag colourful or dark artwork', () => {
    expect(looksLikeBlankPlaceholder(fill([200, 40, 60]))).toBe(false)
    expect(looksLikeBlankPlaceholder(fill([20, 20, 20]))).toBe(false)
    expect(looksLikeBlankPlaceholder(fill([255, 240, 200]))).toBe(false)
  })
})
