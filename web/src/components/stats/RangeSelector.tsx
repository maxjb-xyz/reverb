import { useState } from 'react'
import { presetRange, customRange } from '../../lib/range'
import type { Range, PresetKey } from '../../lib/range'

interface Props {
  value: Range
  onChange: (r: Range) => void
}

const ROLLING_PRESETS: { key: PresetKey; label: string }[] = [
  { key: '7d', label: '7d' },
  { key: '30d', label: '30d' },
  { key: '90d', label: '90d' },
  { key: 'year', label: 'Year' },
  { key: 'all', label: 'All time' },
]

const CALENDAR_PRESETS: { key: PresetKey; label: string }[] = [
  { key: 'thisWeek', label: 'This week' },
  { key: 'thisMonth', label: 'This month' },
  { key: 'thisYear', label: 'This year' },
]

function toInputDate(sec: number): string {
  const d = new Date(sec * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function fromInputDate(s: string): Date {
  const [y, m, d] = s.split('-').map(Number)
  return new Date(y, m - 1, d)
}

/**
 * RangeSelector — preset chips (rolling + calendar-aligned) and a custom
 * date-range picker. Token-styled; no raw hex / text-black / text-white.
 */
export function RangeSelector({ value, onChange }: Props) {
  const [showCustom, setShowCustom] = useState(false)
  // value.from/value.to are always-defined numbers; from:0 (the 'all' preset) is
  // a valid unix second, so drive the inputs from them directly — never `||`,
  // which would treat the falsy 0 as unset and show a 30-day-ago date instead.
  const [fromVal, setFromVal] = useState(() => toInputDate(value.from))
  const [toVal, setToVal] = useState(() => toInputDate(value.to))

  function selectPreset(key: PresetKey) {
    setShowCustom(false)
    onChange(presetRange(key))
  }

  function applyCustom() {
    const start = fromInputDate(fromVal)
    const end = fromInputDate(toVal)
    setShowCustom(false)
    onChange(customRange(start, end))
  }

  function isPresetActive(key: PresetKey): boolean {
    return value.label === presetRange(key).label
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {/* Rolling preset chips */}
      {ROLLING_PRESETS.map(({ key, label }) => {
        const active = isPresetActive(key)
        return (
          <button
            key={key}
            aria-pressed={active}
            onClick={() => selectPreset(key)}
            className={[
              'px-3 py-1 rounded-full text-sm font-medium transition-colors',
              active
                ? 'bg-accent text-on-accent'
                : 'bg-surface-raised text-secondary hover:bg-surface-hover',
            ].join(' ')}
          >
            {label}
          </button>
        )
      })}

      {/* Divider */}
      <span className="w-px h-4 bg-border mx-0.5" aria-hidden />

      {/* Calendar-aligned chips */}
      {CALENDAR_PRESETS.map(({ key, label }) => {
        const active = isPresetActive(key)
        return (
          <button
            key={key}
            aria-pressed={active}
            onClick={() => selectPreset(key)}
            className={[
              'px-3 py-1 rounded-full text-sm font-medium transition-colors',
              active
                ? 'bg-accent text-on-accent'
                : 'bg-surface-raised text-secondary hover:bg-surface-hover',
            ].join(' ')}
          >
            {label}
          </button>
        )
      })}

      {/* Custom range trigger */}
      <button
        aria-pressed={showCustom}
        onClick={() => setShowCustom((v) => !v)}
        className={[
          'px-3 py-1 rounded-full text-sm font-medium transition-colors',
          showCustom
            ? 'bg-accent text-on-accent'
            : 'bg-surface-raised text-secondary hover:bg-surface-hover',
        ].join(' ')}
      >
        Custom
      </button>

      {/* Custom date range popover */}
      {showCustom && (
        <div className="flex flex-wrap items-end gap-2 mt-2 w-full p-3 rounded-lg bg-surface-raised border border-border">
          <div className="flex flex-col gap-1">
            <label htmlFor="range-from" className="text-xs text-secondary">
              From
            </label>
            <input
              id="range-from"
              type="date"
              value={fromVal}
              onChange={(e) => setFromVal(e.target.value)}
              className="px-2 py-1 rounded text-sm bg-surface border border-border text-primary"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor="range-to" className="text-xs text-secondary">
              To
            </label>
            <input
              id="range-to"
              type="date"
              value={toVal}
              onChange={(e) => setToVal(e.target.value)}
              className="px-2 py-1 rounded text-sm bg-surface border border-border text-primary"
            />
          </div>
          <button
            onClick={applyCustom}
            className="px-3 py-1 rounded text-sm font-medium bg-accent text-on-accent hover:bg-accent-hover"
          >
            Apply
          </button>
          <button
            onClick={() => setShowCustom(false)}
            className="px-3 py-1 rounded text-sm text-secondary hover:bg-surface-hover"
          >
            Cancel
          </button>
        </div>
      )}
    </div>
  )
}
