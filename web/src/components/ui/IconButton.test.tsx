import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { IconButton } from './IconButton'

describe('IconButton', () => {
  it('renders with aria-label', () => {
    render(<IconButton name="heart" label="Like" />)
    expect(screen.getByRole('button', { name: 'Like' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Like' }).getAttribute('aria-label')).toBe('Like')
  })

  it('applies text-accent when active', () => {
    render(<IconButton name="heart" label="Like" active />)
    expect(screen.getByRole('button', { name: 'Like' }).className).toMatch(/text-accent/)
  })

  it('does not apply text-accent when inactive', () => {
    render(<IconButton name="heart" label="Like" />)
    expect(screen.getByRole('button', { name: 'Like' }).className).not.toMatch(/text-accent/)
  })

  it('does not fire onClick when disabled', () => {
    const onClick = vi.fn()
    render(<IconButton name="heart" label="Like" disabled onClick={onClick} />)
    fireEvent.click(screen.getByRole('button', { name: 'Like' }))
    expect(onClick).not.toHaveBeenCalled()
  })

  it('fires onClick when enabled', () => {
    const onClick = vi.fn()
    render(<IconButton name="heart" label="Like" onClick={onClick} />)
    fireEvent.click(screen.getByRole('button', { name: 'Like' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('is round (rounded-full)', () => {
    render(<IconButton name="heart" label="Like" />)
    expect(screen.getByRole('button', { name: 'Like' }).className).toMatch(/rounded-full/)
  })

  it('exposes a visible focus ring class', () => {
    render(<IconButton name="heart" label="Like" />)
    expect(screen.getByRole('button', { name: 'Like' }).className).toMatch(/focus-visible:ring/)
  })

  it('is disabled when disabled prop is set', () => {
    render(<IconButton name="heart" label="Like" disabled />)
    expect(screen.getByRole('button', { name: 'Like' })).toBeDisabled()
  })
})
