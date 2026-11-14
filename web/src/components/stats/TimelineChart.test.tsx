import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { TimelineChart } from './TimelineChart'
import type { TimeBucket } from '../../lib/statsApi'

// ── Helpers ────────────────────────────────────────────────────────────────────

function makeBucket(start: number, plays: number, ms = plays * 200_000): TimeBucket {
  return { Start: start, Plays: plays, MsPlayed: ms }
}

const DAY = 86_400
const EPOCH_DAY = 1_700_000_000 // ~2023-11-14 (arbitrary fixed base)

const THREE_BUCKETS: TimeBucket[] = [
  makeBucket(EPOCH_DAY, 5),
  makeBucket(EPOCH_DAY + DAY, 12),
  makeBucket(EPOCH_DAY + DAY * 2, 3),
]

// ── Tests ──────────────────────────────────────────────────────────────────────

describe('TimelineChart', () => {
  describe('bar rendering', () => {
    it('renders one bar rect per bucket', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      // Each bucket gets a data bar (not gridlines/axis etc.)
      // We identify data bars by the data-bar attribute we'll add
      const bars = container.querySelectorAll('[data-bar]')
      expect(bars.length).toBe(3)
    })

    it('the max-value bucket bar is the tallest (has the smallest y value)', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      const bars = Array.from(container.querySelectorAll('[data-bar]'))

      // bucket[1] has 12 plays (the max) → tallest bar → smallest y value in SVG coords
      const ys = bars.map((b) => parseFloat(b.getAttribute('y') ?? '0'))
      const maxBucketIdx = THREE_BUCKETS.reduce(
        (maxIdx, b, i, arr) => (b.Plays > arr[maxIdx].Plays ? i : maxIdx),
        0
      )
      const maxBarY = ys[maxBucketIdx]
      const otherYs = ys.filter((_, i) => i !== maxBucketIdx)
      // In SVG coordinates, smaller y = taller bar (origin at top)
      otherYs.forEach((y) => expect(maxBarY).toBeLessThanOrEqual(y))
    })

    it('bar heights are proportional to play counts', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      const bars = Array.from(container.querySelectorAll('[data-bar]'))
      const heights = bars.map((b) => parseFloat(b.getAttribute('height') ?? '0'))

      // bucket 0: 5 plays, bucket 1: 12 plays, bucket 2: 3 plays
      // height[1] must be greatest, height[2] must be smallest
      expect(heights[1]).toBeGreaterThan(heights[0])
      expect(heights[0]).toBeGreaterThan(heights[2])
    })

    it('renders one bar with a single-bucket dataset', () => {
      const { container } = render(<TimelineChart data={[makeBucket(EPOCH_DAY, 7)]} />)
      const bars = container.querySelectorAll('[data-bar]')
      expect(bars.length).toBe(1)
    })
  })

  describe('ms metric', () => {
    it('renders one bar per bucket when metric=ms', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} metric="ms" />)
      const bars = container.querySelectorAll('[data-bar]')
      expect(bars.length).toBe(3)
    })

    it('in ms mode the max-ms bucket bar is tallest', () => {
      // Give bucket 2 a huge MsPlayed even though it has fewest Plays
      const data: TimeBucket[] = [
        makeBucket(EPOCH_DAY, 5, 500_000),
        makeBucket(EPOCH_DAY + DAY, 12, 1_200_000),
        makeBucket(EPOCH_DAY + DAY * 2, 3, 9_000_000), // biggest ms
      ]
      const { container } = render(<TimelineChart data={data} metric="ms" />)
      const bars = Array.from(container.querySelectorAll('[data-bar]'))
      const ys = bars.map((b) => parseFloat(b.getAttribute('y') ?? '0'))

      // bucket[2] has the highest MsPlayed → smallest y (tallest bar)
      expect(ys[2]).toBeLessThan(ys[0])
      expect(ys[2]).toBeLessThan(ys[1])
    })
  })

  describe('empty state', () => {
    it('shows an empty message when data is empty array', () => {
      render(<TimelineChart data={[]} />)
      expect(screen.getByText(/no listening/i)).toBeDefined()
    })

    it('renders no bar rects when data is empty', () => {
      const { container } = render(<TimelineChart data={[]} />)
      const bars = container.querySelectorAll('[data-bar]')
      expect(bars.length).toBe(0)
    })
  })

  describe('token-only styling', () => {
    it('renders no raw hex colors in SVG output', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      expect(container.innerHTML).not.toMatch(/#[0-9a-fA-F]{3,6}(?=[^;]|$)/)
    })

    it('uses no text-black or text-white classes', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      expect(container.innerHTML).not.toMatch(/\btext-black\b/)
      expect(container.innerHTML).not.toMatch(/\btext-white\b/)
    })

    it('wraps the accent token in rgb() — bare var(--color-accent) is raw channels and renders black', () => {
      const { container } = render(<TimelineChart data={THREE_BUCKETS} />)
      const stops = container.querySelectorAll('stop')
      expect(stops.length).toBeGreaterThan(0)
      // --color-accent is "240 53 75" (channels); stop-color MUST be rgb(var(...))
      // or the gradient is invalid and the bars render black.
      stops.forEach((s) => expect(s.getAttribute('stop-color')).toBe('rgb(var(--color-accent))'))
      expect(container.innerHTML).not.toMatch(/stop-color="var\(--color-accent/)
    })
  })

  // A 30-day range with plays on two days used to draw two fat bars that
  // spanned the whole chart, with no sign of the quiet days in between.
  describe('empty days', () => {
    // Local midnights, so the test holds in any time zone.
    const d0 = new Date(2024, 2, 1)
    const at = (days: number) => Math.floor(new Date(d0.getFullYear(), d0.getMonth(), d0.getDate() + days).getTime() / 1000)

    it('draws a slot for every day of the range, including days without plays', () => {
      const data = [makeBucket(at(2), 4), makeBucket(at(5), 1)]
      const { container } = render(
        <TimelineChart data={data} range={{ from: at(0), to: at(7), bucket: 'day' }} />,
      )
      const bars = Array.from(container.querySelectorAll('[data-bar]'))
      expect(bars.length).toBe(7)
      const heights = bars.map((b) => parseFloat(b.getAttribute('height') ?? '0'))
      expect(heights[2]).toBeGreaterThan(heights[5])
      expect(heights[5]).toBeGreaterThan(0)
      expect(heights[0]).toBe(0)
      expect(heights[6]).toBe(0)
    })

    it('places a bucket whose start is an hour off local midnight (daylight saving) on its own day', () => {
      const data = [makeBucket(at(3) - 3600, 2)]
      const { container } = render(
        <TimelineChart data={data} range={{ from: at(0), to: at(5), bucket: 'day' }} />,
      )
      const heights = Array.from(container.querySelectorAll('[data-bar]')).map((b) => parseFloat(b.getAttribute('height') ?? '0'))
      expect(heights.findIndex((h) => h > 0)).toBe(3)
    })

    it('starts an all-time range at the first month with plays, not 1970', () => {
      const data = [makeBucket(Math.floor(new Date(2024, 0, 1).getTime() / 1000), 3)]
      const to = Math.floor(new Date(2024, 3, 10).getTime() / 1000)
      const { container } = render(<TimelineChart data={data} range={{ from: 0, to, bucket: 'month' }} />)
      expect(container.querySelectorAll('[data-bar]').length).toBe(4) // Jan–Apr
    })
  })

  describe('y axis', () => {
    it('labels whole numbers of plays only, without repeats', () => {
      const { container } = render(<TimelineChart data={[makeBucket(EPOCH_DAY, 1)]} />)
      const labels = Array.from(container.querySelectorAll('[data-ylabel]')).map((n) => n.textContent)
      expect(labels).toEqual(['0', '1'])
    })

    it('uses round steps for larger counts', () => {
      const { container } = render(<TimelineChart data={[makeBucket(EPOCH_DAY, 3), makeBucket(EPOCH_DAY + DAY, 1)]} />)
      const labels = Array.from(container.querySelectorAll('[data-ylabel]')).map((n) => n.textContent)
      expect(labels).toEqual(['0', '1', '2', '3'])
    })
  })
})
