import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Equalizer } from './Equalizer'

describe('Equalizer', () => {
  it('renders 4 animated bars', () => {
    const { container } = render(<Equalizer />)
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars).toHaveLength(4)
  })

  it('each bar has an animation class', () => {
    const { container } = render(<Equalizer />)
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    bars.forEach((bar) => {
      expect(bar.className).toMatch(/animate-/)
    })
  })

  it('accepts a className prop', () => {
    const { container } = render(<Equalizer className="custom-class" />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/custom-class/)
  })

  it('bars use accent color', () => {
    const { container } = render(<Equalizer />)
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    bars.forEach((bar) => {
      expect(bar.className).toMatch(/bg-accent/)
    })
  })
})
