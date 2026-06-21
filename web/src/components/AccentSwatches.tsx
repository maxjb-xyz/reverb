import { useState, useId, useMemo } from 'react'
import { Icon } from './ui'
import { useSettings, useUpdateSettings, applyAccent } from '../lib/settingsApi'

interface Swatch {
  hex: string
  label: string
}

const PRESETS: Swatch[] = [
  { hex: '#F0354B', label: 'Red (default)' },
  { hex: '#7C6AF7', label: 'Indigo' },
  { hex: '#1ed760', label: 'Green' },
  { hex: '#f5a623', label: 'Amber' },
  { hex: '#26d0ce', label: 'Cyan' },
  { hex: '#ff5fa2', label: 'Pink' },
]

function normalizeHex(raw: string): string {
  return raw.startsWith('#') ? raw.toLowerCase() : `#${raw}`.toLowerCase()
}

function hexesMatch(a: string, b: string): boolean {
  return normalizeHex(a) === normalizeHex(b)
}

export function AccentSwatches() {
  const settings = useSettings()
  const currentAccent = settings.data?.accentColor ?? '#F0354B'
  const updateSettings = useUpdateSettings()

  // Initialize custom hex to the current accent when it isn't a preset
  const defaultCustomHex = useMemo(() => {
    const isPreset = PRESETS.some((p) => hexesMatch(p.hex, currentAccent))
    return isPreset ? '#000000' : normalizeHex(currentAccent)
  }, [currentAccent])

  const [customOpen, setCustomOpen] = useState(false)
  const [customValue, setCustomValue] = useState(defaultCustomHex)

  const inputId = useId()

  function selectColor(hex: string) {
    applyAccent(hex)
    updateSettings.mutate({ accentColor: hex })
  }

  function handleCustomChange(e: React.ChangeEvent<HTMLInputElement>) {
    const hex = e.target.value
    setCustomValue(hex)
    if (/^#[0-9a-fA-F]{6}$/.test(hex)) {
      selectColor(hex)
    }
  }

  const customIsActive =
    customOpen && !PRESETS.some((p) => hexesMatch(p.hex, currentAccent))

  return (
    <div className="flex items-center gap-2 flex-wrap">
      {PRESETS.map((swatch) => {
        const isActive = hexesMatch(swatch.hex, currentAccent)
        return (
          <button
            key={swatch.hex}
            type="button"
            aria-label={swatch.label}
            aria-pressed={isActive}
            onClick={() => {
              selectColor(swatch.hex)
              setCustomOpen(false)
            }}
            style={{ background: swatch.hex }}
            className={[
              'w-8 h-8 rounded-full flex-none border-2 border-transparent',
              'hover:scale-110 transition-transform',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-base',
              isActive
                ? 'ring-2 ring-offset-2 ring-offset-base ring-text-primary'
                : '',
            ].join(' ')}
          />
        )
      })}

      {/* Custom-hex swatch */}
      <button
        type="button"
        aria-label="Custom accent color"
        aria-pressed={customIsActive}
        aria-expanded={customOpen}
        onClick={() => setCustomOpen((o) => !o)}
        className={[
          'w-8 h-8 rounded-full flex-none bg-raised-hover border-2 border-border-subtle',
          'flex items-center justify-center text-text-secondary',
          'hover:scale-110 transition-transform',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-base',
          customIsActive ? 'ring-2 ring-offset-2 ring-offset-base ring-text-primary' : '',
        ].join(' ')}
      >
        <Icon name="plus" className="w-4 h-4" />
      </button>

      {customOpen && (
        <div className="flex items-center gap-2">
          <label htmlFor={inputId} className="sr-only">
            Custom hex color
          </label>
          <input
            id={inputId}
            type="text"
            value={customValue}
            onChange={handleCustomChange}
            placeholder="#000000"
            maxLength={7}
            className={[
              'w-28 px-2 py-1 rounded-md bg-input border border-border-subtle',
              'text-sm font-mono text-text-primary',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
            ].join(' ')}
          />
          {/* Live preview swatch */}
          {/^#[0-9a-fA-F]{6}$/.test(customValue) && (
            <span
              aria-hidden="true"
              className="w-6 h-6 rounded-full flex-none border border-border-subtle"
              style={{ background: customValue }}
            />
          )}
        </div>
      )}
    </div>
  )
}
