import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { RangeSelector } from './RangeSelector'
import { presetRange, customRange } from '../../lib/range'
import type { Range } from '../../lib/range'

function makeRange(): Range {
  return presetRange('30d')
}

describe('RangeSelector', () => {
  describe('preset chips', () => {
    it('renders all rolling preset chips', () => {
      render(<RangeSelector value={makeRange()} onChange={vi.fn()} />)
      expect(screen.getByRole('button', { name: /^7d$/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /^30d$/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /^90d$/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /^Year$/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /^All time$/i })).toBeDefined()
    })

    it('renders calendar-aligned chips', () => {
      render(<RangeSelector value={makeRange()} onChange={vi.fn()} />)
      expect(screen.getByRole('button', { name: /this week/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /this month/i })).toBeDefined()
      expect(screen.getByRole('button', { name: /this year/i })).toBeDefined()
    })

    it('clicking 7d chip calls onChange with presetRange("7d")', () => {
      const onChange = vi.fn()
      render(<RangeSelector value={makeRange()} onChange={onChange} />)
      fireEvent.click(screen.getByRole('button', { name: /7d|Last 7 days/i }))
      expect(onChange).toHaveBeenCalledTimes(1)
      const called = onChange.mock.calls[0][0] as Range
      const expected = presetRange('7d')
      // from/to may differ by a second or two; check bucket and label
      expect(called.bucket).toBe(expected.bucket)
      expect(called.label).toBe('Last 7 days')
    })

    it('clicking 90d chip calls onChange with presetRange("90d")', () => {
      const onChange = vi.fn()
      render(<RangeSelector value={makeRange()} onChange={onChange} />)
      fireEvent.click(screen.getByRole('button', { name: /90d|Last 90 days/i }))
      expect(onChange).toHaveBeenCalledTimes(1)
      const called = onChange.mock.calls[0][0] as Range
      expect(called.label).toBe('Last 90 days')
      expect(called.bucket).toBe('week')
    })

    it('clicking "This month" chip calls onChange with presetRange("thisMonth")', () => {
      const onChange = vi.fn()
      render(<RangeSelector value={makeRange()} onChange={onChange} />)
      fireEvent.click(screen.getByRole('button', { name: /this month/i }))
      expect(onChange).toHaveBeenCalledTimes(1)
      const called = onChange.mock.calls[0][0] as Range
      expect(called.label).toBe('This month')
    })

    it('active chip is visually highlighted (aria-pressed=true)', () => {
      const active = presetRange('30d')
      render(<RangeSelector value={active} onChange={vi.fn()} />)
      // The active chip should be aria-pressed=true
      const buttons = screen.getAllByRole('button')
      const pressedButtons = buttons.filter(
        (b) => b.getAttribute('aria-pressed') === 'true'
      )
      expect(pressedButtons.length).toBeGreaterThan(0)
    })
  })

  describe('custom date range', () => {
    it('shows a custom range option', () => {
      render(<RangeSelector value={makeRange()} onChange={vi.fn()} />)
      // Custom trigger button exists
      const customButton = screen.queryByRole('button', { name: /custom/i })
      expect(customButton).not.toBeNull()
    })

    it('selecting custom dates calls onChange with customRange result', () => {
      const onChange = vi.fn()
      render(<RangeSelector value={makeRange()} onChange={onChange} />)

      // Open the custom range picker
      fireEvent.click(screen.getByRole('button', { name: /custom/i }))

      // Fill in the date inputs
      const fromInput = screen.getByLabelText(/from|start/i)
      const toInput = screen.getByLabelText(/to|end/i)

      fireEvent.change(fromInput, { target: { value: '2024-03-01' } })
      fireEvent.change(toInput, { target: { value: '2024-03-07' } })

      // Submit / apply
      const applyBtn = screen.getByRole('button', { name: /apply/i })
      fireEvent.click(applyBtn)

      expect(onChange).toHaveBeenCalledTimes(1)
      const called = onChange.mock.calls[0][0] as Range
      const expected = customRange(new Date(2024, 2, 1), new Date(2024, 2, 7))
      expect(called.from).toBe(expected.from)
      expect(called.to).toBe(expected.to)
      expect(called.bucket).toBe('day')
    })
  })

  describe('token-only styling', () => {
    it('JSX contains no raw hex colours', () => {
      // This is enforced at review time, but we can check rendered output
      const { container } = render(<RangeSelector value={makeRange()} onChange={vi.fn()} />)
      expect(container.innerHTML).not.toMatch(/#[0-9a-fA-F]{3,6}/)
    })

    it('JSX uses no text-black or text-white classes', () => {
      const { container } = render(<RangeSelector value={makeRange()} onChange={vi.fn()} />)
      expect(container.innerHTML).not.toMatch(/\btext-black\b/)
      expect(container.innerHTML).not.toMatch(/\btext-white\b/)
    })
  })
})
