import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { ProgressRing } from './ProgressRing'

const CIRCUMFERENCE = 2 * Math.PI * 15 // ≈ 94.25

describe('ProgressRing', () => {
  it('renders an SVG element', () => {
    const { container } = render(<ProgressRing value={50} />)
    expect(container.querySelector('svg')).toBeInTheDocument()
  })

  it('sets stroke-dasharray to the circumference', () => {
    const { container } = render(<ProgressRing value={50} />)
    const circles = container.querySelectorAll('circle')
    // The progress circle (second one) has the dasharray
    const progressCircle = circles[1]
    const dasharray = progressCircle.getAttribute('stroke-dasharray')
    expect(parseFloat(dasharray!)).toBeCloseTo(CIRCUMFERENCE, 0)
  })

  it('sets stroke-dashoffset near circumference when value=0 (full ring hidden)', () => {
    const { container } = render(<ProgressRing value={0} />)
    const circles = container.querySelectorAll('circle')
    const progressCircle = circles[1]
    const offset = parseFloat(progressCircle.getAttribute('stroke-dashoffset')!)
    expect(offset).toBeCloseTo(CIRCUMFERENCE, 0)
  })

  it('sets stroke-dashoffset near 0 when value=100 (full ring shown)', () => {
    const { container } = render(<ProgressRing value={100} />)
    const circles = container.querySelectorAll('circle')
    const progressCircle = circles[1]
    const offset = parseFloat(progressCircle.getAttribute('stroke-dashoffset')!)
    expect(offset).toBeCloseTo(0, 0)
  })

  it('sets stroke-dashoffset proportional for value=50 (half ring)', () => {
    const { container } = render(<ProgressRing value={50} />)
    const circles = container.querySelectorAll('circle')
    const progressCircle = circles[1]
    const offset = parseFloat(progressCircle.getAttribute('stroke-dashoffset')!)
    expect(offset).toBeCloseTo(CIRCUMFERENCE * 0.5, 0)
  })

  it('applies accent stroke to the progress arc', () => {
    const { container } = render(<ProgressRing value={60} />)
    const circles = container.querySelectorAll('circle')
    const progressCircle = circles[1]
    // stroke is set via className (text-accent + stroke-current) or inline
    const stroke = progressCircle.getAttribute('stroke')
    const className = progressCircle.getAttribute('class') ?? ''
    const hasAccent = stroke === 'currentColor' || className.includes('accent')
    expect(hasAccent).toBe(true)
  })

  it('uses default size of 36 when size not specified', () => {
    const { container } = render(<ProgressRing value={50} />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('36')
    expect(svg.getAttribute('height')).toBe('36')
  })

  it('uses custom size when specified', () => {
    const { container } = render(<ProgressRing value={50} size={48} />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('48')
    expect(svg.getAttribute('height')).toBe('48')
  })

  it('rotates the progress ring by -90deg so it starts at the top', () => {
    const { container } = render(<ProgressRing value={50} />)
    const circles = container.querySelectorAll('circle')
    const progressCircle = circles[1]
    const transform = progressCircle.getAttribute('transform') ?? ''
    expect(transform).toMatch(/rotate\(-?90/)
  })

  // ── indeterminate variant ──────────────────────────────────────────────────

  it('renders a partial arc when indeterminate=true', () => {
    const { container } = render(<ProgressRing value={0} indeterminate />)
    const circles = container.querySelectorAll('circle')
    const arc = circles[1]
    const dasharray = arc.getAttribute('stroke-dasharray') ?? ''
    // Should contain two numbers (dash gap), not just one (full dasharray)
    expect(dasharray).toMatch(/[\d.]+\s+[\d.]+/)
  })

  it('indeterminate arc has aria-label "Loading" and aria-busy', () => {
    const { container } = render(<ProgressRing value={0} indeterminate />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-label')).toBe('Loading')
    expect(svg.getAttribute('aria-busy')).toBe('true')
  })

  it('indeterminate arc has motion-safe:animate-spin class (spins unless reduced-motion)', () => {
    const { container } = render(<ProgressRing value={0} indeterminate />)
    const circles = container.querySelectorAll('circle')
    const arc = circles[1]
    const className = arc.getAttribute('class') ?? ''
    expect(className).toContain('motion-safe:animate-spin')
  })

  it('indeterminate uses custom size', () => {
    const { container } = render(<ProgressRing value={0} size={24} indeterminate />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('width')).toBe('24')
  })

  it('non-indeterminate ring does not have aria-busy', () => {
    const { container } = render(<ProgressRing value={50} />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-busy')).toBeNull()
  })
})
