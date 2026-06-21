import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Chip } from './Chip'

describe('Chip', () => {
  it('applies selected classes when selected', () => {
    render(<Chip selected>Playlists</Chip>)
    const chip = screen.getByText('Playlists')
    expect(chip.className).toMatch(/bg-text-primary/)
    expect(chip.className).toMatch(/text-base/)
  })

  it('applies unselected classes when not selected', () => {
    render(<Chip>Playlists</Chip>)
    const chip = screen.getByText('Playlists')
    expect(chip.className).toMatch(/bg-raised/)
    expect(chip.className).toMatch(/text-text-primary/)
  })

  it('fires onClick when clicked', () => {
    const onClick = vi.fn()
    render(<Chip onClick={onClick}>Albums</Chip>)
    fireEvent.click(screen.getByText('Albums'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('is pill-shaped (rounded-full)', () => {
    render(<Chip>Artists</Chip>)
    expect(screen.getByText('Artists').className).toMatch(/rounded-full/)
  })

  it('exposes a visible focus ring class', () => {
    render(<Chip>Playlists</Chip>)
    expect(screen.getByText('Playlists').className).toMatch(/focus-visible:ring/)
  })
})
