import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Button } from './Button'

describe('Button', () => {
  it('applies the primary accent style', () => {
    render(<Button variant="primary">Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' }).className).toMatch(/bg-accent/)
  })

  it('applies black text on primary', () => {
    render(<Button variant="primary">Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' }).className).toMatch(/text-black/)
  })

  it('applies secondary bordered style', () => {
    render(<Button variant="secondary">Go</Button>)
    const btn = screen.getByRole('button', { name: 'Go' })
    expect(btn.className).toMatch(/border/)
    expect(btn.className).toMatch(/border-border-subtle/)
  })

  it('applies ghost text-only style', () => {
    render(<Button variant="ghost">Go</Button>)
    const btn = screen.getByRole('button', { name: 'Go' })
    expect(btn.className).toMatch(/text-text-secondary/)
  })

  it('does not fire onClick when disabled', () => {
    const onClick = vi.fn()
    render(<Button disabled onClick={onClick}>Go</Button>)
    fireEvent.click(screen.getByRole('button'))
    expect(onClick).not.toHaveBeenCalled()
  })

  it('fires onClick when enabled', () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Go</Button>)
    fireEvent.click(screen.getByRole('button'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('exposes a visible focus ring class', () => {
    render(<Button>Go</Button>)
    expect(screen.getByRole('button').className).toMatch(/focus-visible:ring/)
  })

  it('is pill-shaped (rounded-full)', () => {
    render(<Button>Go</Button>)
    expect(screen.getByRole('button').className).toMatch(/rounded-full/)
  })

  it('is disabled when disabled prop is set', () => {
    render(<Button disabled>Go</Button>)
    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('applies sm size class', () => {
    render(<Button size="sm">Go</Button>)
    const btn = screen.getByRole('button', { name: 'Go' })
    expect(btn.className).toMatch(/text-sm/)
  })
})
