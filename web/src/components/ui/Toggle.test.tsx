import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Toggle } from './Toggle'

describe('Toggle', () => {
  it('renders with role=switch', () => {
    render(<Toggle checked={false} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch')).toBeTruthy()
  })

  it('sets aria-label from label prop', () => {
    render(<Toggle checked={false} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').getAttribute('aria-label')).toBe('Autoplay')
  })

  it('sets aria-checked=true when checked', () => {
    render(<Toggle checked={true} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('true')
  })

  it('sets aria-checked=false when unchecked', () => {
    render(<Toggle checked={false} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false')
  })

  it('applies bg-accent when checked (on state)', () => {
    render(<Toggle checked={true} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').className).toMatch(/bg-accent/)
  })

  it('does not apply bg-accent when unchecked', () => {
    render(<Toggle checked={false} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').className).not.toMatch(/bg-accent/)
  })

  it('calls onChange with toggled value when clicked', () => {
    const onChange = vi.fn()
    render(<Toggle checked={false} onChange={onChange} label="Autoplay" />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('calls onChange with false when checked and clicked', () => {
    const onChange = vi.fn()
    render(<Toggle checked={true} onChange={onChange} label="Autoplay" />)
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenCalledWith(false)
  })

  it('exposes a visible focus ring class', () => {
    render(<Toggle checked={false} onChange={vi.fn()} label="Autoplay" />)
    expect(screen.getByRole('switch').className).toMatch(/focus-visible:ring/)
  })
})
