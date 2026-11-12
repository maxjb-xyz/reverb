import { useState } from 'react'
import { presetRange, customRange } from '../../lib/range'
import type { Range, PresetKey } from '../../lib/range'
import { Chip } from '../ui/Chip'
import { Button } from '../ui/Button'

interface Props {
  value: Range
  onChange: (r: Range) => void
}

// Five choices cover what people actually ask of a stats page; the calendar
// and 90-day variants were near-duplicates that made the row wrap on phones.
const PRESETS: { key: PresetKey; label: string }[] = [
  { key: '7d', label: '7 days' },
  { key: '30d', label: '30 days' },
  { key: 'thisYear', label: 'This year' },
  { key: 'all', label: 'All time' },
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
 * RangeSelector — four preset chips and a custom
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
  const isCustom = !PRESETS.some(({ key }) => isPresetActive(key))

  return (
    <div className="flex flex-wrap items-center gap-2">
      {PRESETS.map(({ key, label }) => (
        <Chip key={key} asToggle selected={!showCustom && isPresetActive(key)} onClick={() => selectPreset(key)}>
          {label}
        </Chip>
      ))}
      <Chip asToggle selected={showCustom || isCustom} onClick={() => setShowCustom((v) => !v)}>
        Custom
      </Chip>

      {/* Custom date range popover */}
      {showCustom && (
        <div className="flex flex-wrap items-end gap-2 mt-2 w-full p-3 rounded-lg bg-raised border border-border">
          <div className="flex flex-col gap-1">
            <label htmlFor="range-from" className="text-xs text-text-muted">
              From
            </label>
            <input
              id="range-from"
              type="date"
              value={fromVal}
              onChange={(e) => setFromVal(e.target.value)}
              className="px-2 py-1 rounded text-sm bg-surface border border-border text-text-primary"
            />
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor="range-to" className="text-xs text-text-muted">
              To
            </label>
            <input
              id="range-to"
              type="date"
              value={toVal}
              onChange={(e) => setToVal(e.target.value)}
              className="px-2 py-1 rounded text-sm bg-surface border border-border text-text-primary"
            />
          </div>
          <Button size="sm" variant="primary" onClick={applyCustom}>
            Apply
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setShowCustom(false)}>
            Cancel
          </Button>
        </div>
      )}
    </div>
  )
}
