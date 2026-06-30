import { describe, it, expect } from 'vitest'
import { presetRange, customRange } from './range'
import type { Range } from './range'

// Fixed reference point: 2024-03-15 12:00:00 UTC (a Friday)
// We'll use a fixed Date for deterministic tests
const FIXED_NOW = new Date('2024-03-15T12:00:00Z')

describe('presetRange', () => {
  describe('rolling presets', () => {
    it('7d: to≈now, from≈now-7days, bucket=day', () => {
      const r = presetRange('7d', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      expect(r.to).toBe(nowSec)
      expect(r.from).toBe(nowSec - 7 * 86400)
      expect(r.bucket).toBe('day')
      expect(r.label).toBe('Last 7 days')
    })

    it('30d: to≈now, from≈now-30days, bucket=day', () => {
      const r = presetRange('30d', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      expect(r.to).toBe(nowSec)
      expect(r.from).toBe(nowSec - 30 * 86400)
      expect(r.bucket).toBe('day')
      expect(r.label).toBe('Last 30 days')
    })

    it('90d: bucket=week (span=90d > 45d threshold)', () => {
      const r = presetRange('90d', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      expect(r.to).toBe(nowSec)
      expect(r.from).toBe(nowSec - 90 * 86400)
      expect(r.bucket).toBe('week')
      expect(r.label).toBe('Last 90 days')
    })

    it('year: bucket=week (span=365d)', () => {
      const r = presetRange('year', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      expect(r.to).toBe(nowSec)
      expect(r.from).toBe(nowSec - 365 * 86400)
      expect(r.bucket).toBe('week')
      expect(r.label).toBe('Last year')
    })

    it('all: from=0, bucket=month', () => {
      const r = presetRange('all', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      expect(r.from).toBe(0)
      expect(r.to).toBe(nowSec)
      expect(r.bucket).toBe('month')
      expect(r.label).toBe('All time')
    })
  })

  describe('calendar-aligned presets', () => {
    it('thisWeek: from = local Monday 00:00 of current week', () => {
      // FIXED_NOW = 2024-03-15 (Friday). Monday of that week = 2024-03-11.
      const r = presetRange('thisWeek', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      // Monday in local time: new Date(2024, 2, 11) — month is 0-indexed
      const localMonday = new Date(2024, 2, 11) // local midnight Mon Mar 11
      const expectedFrom = Math.floor(localMonday.getTime() / 1000)
      expect(r.from).toBe(expectedFrom)
      expect(r.to).toBe(nowSec)
      expect(r.bucket).toBe('day')
      expect(r.label).toBe('This week')
    })

    it('thisMonth: from = local 1st of month 00:00', () => {
      // FIXED_NOW = 2024-03-15. First of March in local time.
      const r = presetRange('thisMonth', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      const localFirst = new Date(2024, 2, 1) // local midnight Mar 1
      const expectedFrom = Math.floor(localFirst.getTime() / 1000)
      expect(r.from).toBe(expectedFrom)
      expect(r.to).toBe(nowSec)
      expect(r.bucket).toBe('day')
      expect(r.label).toBe('This month')
    })

    it('thisYear: from = local Jan 1 00:00 of current year', () => {
      const r = presetRange('thisYear', FIXED_NOW)
      const nowSec = Math.floor(FIXED_NOW.getTime() / 1000)
      const localJan1 = new Date(2024, 0, 1) // local midnight Jan 1 2024
      const expectedFrom = Math.floor(localJan1.getTime() / 1000)
      expect(r.from).toBe(expectedFrom)
      expect(r.to).toBe(nowSec)
      // span from Jan 1 to Mar 15 ≈ 74 days → autoBucket gives 'week'
      expect(r.bucket).toBe('week')
      expect(r.label).toBe('This year')
    })
  })

  describe('tzOffsetMinutes', () => {
    it('equals -new Date().getTimezoneOffset()', () => {
      const r = presetRange('7d', FIXED_NOW)
      expect(r.tzOffsetMinutes).toBe(-FIXED_NOW.getTimezoneOffset())
    })
  })

  describe('bucket auto by span', () => {
    it('span ≤ 45 days → day', () => {
      const r = presetRange('30d', FIXED_NOW)
      expect(r.bucket).toBe('day')
    })

    it('span 46–550 days → week', () => {
      const r = presetRange('90d', FIXED_NOW)
      expect(r.bucket).toBe('week')
    })

    it('span > 550 days → month (all time)', () => {
      const r = presetRange('all', FIXED_NOW)
      expect(r.bucket).toBe('month')
    })
  })
})

describe('customRange', () => {
  it('includes the full start day (local midnight)', () => {
    const start = new Date(2024, 2, 1) // Mar 1 local midnight
    const end = new Date(2024, 2, 7) // Mar 7 local midnight
    const r = customRange(start, end)
    const expectedFrom = Math.floor(new Date(2024, 2, 1).getTime() / 1000)
    expect(r.from).toBe(expectedFrom)
  })

  it('end day is inclusive (to = startOfDay(end) + 86400)', () => {
    const start = new Date(2024, 2, 1)
    const end = new Date(2024, 2, 7)
    const r = customRange(start, end)
    const expectedTo = Math.floor(new Date(2024, 2, 7).getTime() / 1000) + 86400
    expect(r.to).toBe(expectedTo)
  })

  it('has a label', () => {
    const start = new Date(2024, 2, 1)
    const end = new Date(2024, 2, 7)
    const r = customRange(start, end)
    expect(r.label).toBeTruthy()
  })

  it('bucket auto by span (6-day span → day)', () => {
    const start = new Date(2024, 2, 1)
    const end = new Date(2024, 2, 7)
    const r = customRange(start, end)
    expect(r.bucket).toBe('day')
  })

  it('tzOffsetMinutes set correctly', () => {
    const start = new Date(2024, 2, 1)
    const end = new Date(2024, 2, 7)
    const r = customRange(start, end)
    expect(r.tzOffsetMinutes).toBe(-start.getTimezoneOffset())
  })

  it('result satisfies Range shape', () => {
    const r = customRange(new Date(2024, 0, 1), new Date(2024, 11, 31))
    const shape: Range = r
    expect(typeof shape.from).toBe('number')
    expect(typeof shape.to).toBe('number')
    expect(['day', 'week', 'month']).toContain(shape.bucket)
    expect(typeof shape.tzOffsetMinutes).toBe('number')
    expect(typeof shape.label).toBe('string')
  })
})
