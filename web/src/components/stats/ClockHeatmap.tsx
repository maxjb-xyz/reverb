import { useState } from 'react'
import type { ClockCell } from '../../lib/statsApi'

// ── Types ──────────────────────────────────────────────────────────────────────

interface Props {
  data: ClockCell[]
}

interface TooltipState {
  weekday: number
  hour: number
  plays: number
  svgX: number
  svgY: number
}

// ── Constants ──────────────────────────────────────────────────────────────────

// Display order: Monday-first (Mon=0 in display, Sun=6 in display)
// API: Weekday 0=Sunday..6=Saturday
// Mon-first display mapping: display row = (weekday + 6) % 7
//   Sun(0) → row 6, Mon(1) → row 0, Tue(2) → row 1, ... Sat(6) → row 5
const WEEKDAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
const HOUR_LABEL_TICKS = [0, 6, 12, 18]

// SVG layout
const CELL_W = 20
const CELL_H = 18
const CELL_GAP = 2
const ROW_LABEL_W = 28
const COL_LABEL_H = 18
const VIEW_PAD = 4

const GRID_W = 24 * (CELL_W + CELL_GAP) - CELL_GAP
const GRID_H = 7 * (CELL_H + CELL_GAP) - CELL_GAP

const VIEW_W = ROW_LABEL_W + GRID_W + VIEW_PAD * 2
const VIEW_H = COL_LABEL_H + GRID_H + VIEW_PAD * 2

const GRID_X = ROW_LABEL_W + VIEW_PAD
const GRID_Y = COL_LABEL_H + VIEW_PAD

// ── Helpers ────────────────────────────────────────────────────────────────────

/** Build the full 7×24 grid from the sparse API cells (missing = 0 plays). */
function buildGrid(data: ClockCell[]): number[][] {
  // grid[weekday0..6][hour0..23] = plays
  const grid: number[][] = Array.from({ length: 7 }, () => new Array(24).fill(0))
  for (const cell of data) {
    if (cell.Weekday >= 0 && cell.Weekday <= 6 && cell.Hour >= 0 && cell.Hour <= 23) {
      grid[cell.Weekday][cell.Hour] = cell.Plays
    }
  }
  return grid
}

function displayRow(weekday: number): number {
  // Mon-first: Mon(1)→0, Tue(2)→1, ... Sat(6)→5, Sun(0)→6
  return (weekday + 6) % 7
}

function formatTooltip(weekday: number, hour: number, plays: number): string {
  const day = WEEKDAY_LABELS[displayRow(weekday)]
  const hStr = String(hour).padStart(2, '0')
  return `${day} ${hStr}:00 · ${plays} play${plays === 1 ? '' : 's'}`
}

/** Minimum opacity for non-zero cells so they're still perceptible. */
const MIN_OPACITY = 0.12

function cellOpacity(plays: number, maxPlays: number): number {
  if (maxPlays === 0 || plays === 0) return 0
  // Scale from MIN_OPACITY to 1.0
  return MIN_OPACITY + (1 - MIN_OPACITY) * (plays / maxPlays)
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ClockHeatmap({ data }: Props) {
  const [tooltip, setTooltip] = useState<TooltipState | null>(null)

  const grid = buildGrid(data)
  const maxPlays = Math.max(...grid.flatMap((row) => row), 0)

  return (
    <div className="relative w-full select-none overflow-x-auto">
      <svg
        viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
        width="100%"
        aria-label="When you listen — hour by weekday heatmap"
        style={{ overflow: 'visible', minWidth: 320 }}
      >
        {/* Hour column labels */}
        {HOUR_LABEL_TICKS.map((h) => (
          <text
            key={h}
            x={GRID_X + h * (CELL_W + CELL_GAP) + CELL_W / 2}
            y={VIEW_PAD + COL_LABEL_H - 4}
            textAnchor="middle"
            fontSize={9}
            fill="var(--text-muted)"
          >
            {h}
          </text>
        ))}

        {/* Weekday row labels + cell rows */}
        {WEEKDAY_LABELS.map((dayLabel, displayRowIdx) => {
          // Map display row back to API weekday
          // displayRow(weekday) = (weekday + 6) % 7 = displayRowIdx
          // → weekday = (displayRowIdx + 1) % 7
          const weekday = (displayRowIdx + 1) % 7

          const rowY = GRID_Y + displayRowIdx * (CELL_H + CELL_GAP)

          return (
            <g key={dayLabel}>
              {/* Row label */}
              <text
                x={VIEW_PAD + ROW_LABEL_W - 4}
                y={rowY + CELL_H / 2}
                textAnchor="end"
                dominantBaseline="middle"
                fontSize={9}
                fill="var(--text-muted)"
              >
                {dayLabel}
              </text>

              {/* 24 hour cells */}
              {Array.from({ length: 24 }, (_, hour) => {
                const plays = grid[weekday][hour]
                const opacity = cellOpacity(plays, maxPlays)
                const x = GRID_X + hour * (CELL_W + CELL_GAP)

                return (
                  <rect
                    key={hour}
                    data-cell
                    data-weekday={weekday}
                    data-hour={hour}
                    x={x}
                    y={rowY}
                    width={CELL_W}
                    height={CELL_H}
                    rx={3}
                    fill="rgb(var(--color-accent))"
                    fillOpacity={opacity}
                    style={{
                      cursor: plays > 0 ? 'default' : 'default',
                      // Base cell — always visible as a subtle surface
                      outline: 'none',
                    }}
                    onMouseEnter={(e) => {
                      const svgEl = (e.currentTarget as SVGElement).closest('svg')!
                      const svgRect = svgEl.getBoundingClientRect()
                      const svgX = (x + CELL_W / 2) / VIEW_W * svgRect.width
                      const svgY = rowY / VIEW_H * svgRect.height
                      setTooltip({ weekday, hour, plays, svgX, svgY })
                    }}
                    onMouseLeave={() => setTooltip(null)}
                  />
                )
              })}
            </g>
          )
        })}

        {/* Subtle cell background layer — render BEHIND the accent fill */}
        {/* This is done by rendering a background rect for zero-play cells */}
        {/* Actually handled via low fillOpacity + the overall container bg */}
      </svg>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 px-2.5 py-1.5 rounded-md bg-raised shadow-float text-xs font-semibold text-primary whitespace-nowrap"
          style={{
            left: tooltip.svgX,
            top: tooltip.svgY - 8,
            transform: 'translate(-50%, -100%)',
          }}
        >
          {formatTooltip(tooltip.weekday, tooltip.hour, tooltip.plays)}
        </div>
      )}
    </div>
  )
}
