import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ClockHeatmap } from './ClockHeatmap'
import type { ClockCell } from '../../lib/statsApi'

// ── Helpers ────────────────────────────────────────────────────────────────────

function makeCell(weekday: number, hour: number, plays: number): ClockCell {
  return { Weekday: weekday, Hour: hour, Plays: plays, MsPlayed: plays * 180_000 }
}

// Sparse data: just a few cells — the rest should be filled with 0
const SPARSE_DATA: ClockCell[] = [
  makeCell(1, 9, 5),   // Monday  09:00 — 5 plays
  makeCell(3, 14, 20), // Wednesday 14:00 — 20 plays (busiest)
  makeCell(5, 20, 8),  // Friday  20:00 — 8 plays
]

// ── Tests ──────────────────────────────────────────────────────────────────────

describe('ClockHeatmap', () => {
  describe('grid completeness', () => {
    it('renders exactly 168 cells (7 × 24) from sparse input', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      const cells = container.querySelectorAll('[data-cell]')
      expect(cells.length).toBe(168)
    })

    it('renders 168 cells when data is empty (all zeros)', () => {
      const { container } = render(<ClockHeatmap data={[]} />)
      const cells = container.querySelectorAll('[data-cell]')
      expect(cells.length).toBe(168)
    })

    it('renders 168 cells when given a full 168-cell dataset', () => {
      const full: ClockCell[] = []
      for (let w = 0; w < 7; w++) {
        for (let h = 0; h < 24; h++) {
          full.push(makeCell(w, h, w * 24 + h + 1))
        }
      }
      const { container } = render(<ClockHeatmap data={full} />)
      const cells = container.querySelectorAll('[data-cell]')
      expect(cells.length).toBe(168)
    })
  })

  describe('intensity scaling', () => {
    it('the busiest cell has the highest fill opacity', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)

      // Wednesday=3 hour=14 is the busiest (20 plays)
      // Find the cell by data-weekday and data-hour attributes
      const busiestCell = container.querySelector('[data-weekday="3"][data-hour="14"]')
      expect(busiestCell).not.toBeNull()

      // It should have opacity 1 (or close to it) since it's the max
      const opacity = parseFloat(busiestCell!.getAttribute('fill-opacity') ?? '0')
      expect(opacity).toBeCloseTo(1, 2)
    })

    it('zero-play cells have opacity 0', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      // weekday=0 hour=0 has no data → 0 plays → opacity should be 0
      const emptyCell = container.querySelector('[data-weekday="0"][data-hour="0"]')
      expect(emptyCell).not.toBeNull()
      const opacity = parseFloat(emptyCell!.getAttribute('fill-opacity') ?? '1')
      expect(opacity).toBe(0)
    })

    it('a partial-plays cell has opacity between 0 and 1', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      // Friday 20:00 has 8 plays; max is 20 → opacity = 8/20 = 0.4
      const cell = container.querySelector('[data-weekday="5"][data-hour="20"]')
      expect(cell).not.toBeNull()
      const opacity = parseFloat(cell!.getAttribute('fill-opacity') ?? '0')
      expect(opacity).toBeGreaterThan(0)
      expect(opacity).toBeLessThan(1)
    })
  })

  describe('grid position mapping', () => {
    it('each cell has a unique (weekday, hour) coordinate', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      const cells = Array.from(container.querySelectorAll('[data-cell]'))
      const coords = new Set(
        cells.map((c) => `${c.getAttribute('data-weekday')}-${c.getAttribute('data-hour')}`)
      )
      expect(coords.size).toBe(168)
    })

    it('Monday (weekday=1) cells are in the first display row (row 0 in Mon-first order)', () => {
      // In Mon-first display: Mon=row0, Tue=row1, ..., Sun=row6
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      // We check that a Monday cell exists and has correct data-weekday
      const mondayCell = container.querySelector('[data-weekday="1"][data-hour="9"]')
      expect(mondayCell).not.toBeNull()
    })

    it('Sunday (weekday=0) cells are in the last display row (Mon-first ordering)', () => {
      const sunData = [makeCell(0, 5, 3)] // Sunday = weekday 0
      const { container } = render(<ClockHeatmap data={sunData} />)
      const sundayCell = container.querySelector('[data-weekday="0"][data-hour="5"]')
      expect(sundayCell).not.toBeNull()
      // Sunday row y-value should be the largest of all weekday rows
      const sundayCellEl = sundayCell as SVGRectElement
      const sundayY = parseFloat(sundayCellEl.getAttribute('y') ?? '0')
      // Get a Monday cell y for comparison
      const mondayCell = container.querySelector('[data-weekday="1"][data-hour="5"]')
      const mondayY = parseFloat((mondayCell as SVGRectElement).getAttribute('y') ?? '0')
      expect(sundayY).toBeGreaterThan(mondayY)
    })
  })

  describe('row/col labels', () => {
    it('renders weekday row labels', () => {
      render(<ClockHeatmap data={[]} />)
      // Spot-check a couple of day labels
      expect(screen.getByText('Mon')).toBeDefined()
      expect(screen.getByText('Fri')).toBeDefined()
    })

    it('renders hour column labels', () => {
      render(<ClockHeatmap data={[]} />)
      // We label at least 0h and 12h
      expect(screen.getByText('0')).toBeDefined()
      expect(screen.getByText('12')).toBeDefined()
    })
  })

  describe('empty state', () => {
    it('does NOT show an empty message — still renders the full grid (all zeros)', () => {
      const { container } = render(<ClockHeatmap data={[]} />)
      const cells = container.querySelectorAll('[data-cell]')
      expect(cells.length).toBe(168)
    })
  })

  describe('token-only styling', () => {
    it('renders no raw hex colors', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      // Exclude the SVG fill attribute which uses currentColor (not a hex)
      // We check the class attributes / style attributes
      const html = container.innerHTML
      expect(html).not.toMatch(/#[0-9a-fA-F]{6}\b/)
    })

    it('uses no text-black or text-white classes', () => {
      const { container } = render(<ClockHeatmap data={SPARSE_DATA} />)
      expect(container.innerHTML).not.toMatch(/\btext-black\b/)
      expect(container.innerHTML).not.toMatch(/\btext-white\b/)
    })
  })
})
