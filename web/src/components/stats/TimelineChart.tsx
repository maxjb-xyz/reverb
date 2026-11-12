import { useState, useId } from 'react'
import type { TimeBucket } from '../../lib/statsApi'

// ── Types ──────────────────────────────────────────────────────────────────────

type Granularity = 'day' | 'week' | 'month'

interface Props {
  data: TimeBucket[]
  metric?: 'plays' | 'ms'
  /** The requested range. With it, every day/week/month gets a slot, played or not. */
  range?: { from: number; to: number; bucket: Granularity }
}

interface Tooltip {
  x: number
  y: number
  label: string
  value: string
  barX: number
  barWidth: number
}

// ── Helpers ────────────────────────────────────────────────────────────────────

/** Local start (unix seconds) of the day/week (Monday)/month containing sec. */
function slotFloor(sec: number, g: Granularity): Date {
  const d = new Date(sec * 1000)
  if (g === 'month') return new Date(d.getFullYear(), d.getMonth(), 1)
  if (g === 'week') return new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7))
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}

function nextSlot(d: Date, g: Granularity): Date {
  if (g === 'month') return new Date(d.getFullYear(), d.getMonth() + 1, 1)
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + (g === 'week' ? 7 : 1))
}

const MAX_SLOTS = 400

/**
 * fillBuckets lays the server's non-empty buckets onto every day/week/month of
 * the range, so quiet days show as gaps instead of disappearing. An all-time
 * range (from 0) starts at the first bucket with plays. Buckets are matched to
 * slots by their middle, not their exact start, so a start that is an hour off
 * local midnight (the server uses one fixed UTC offset across daylight saving)
 * still lands on its own day.
 */
function fillBuckets(data: TimeBucket[], range: { from: number; to: number; bucket: Granularity }): TimeBucket[] {
  if (data.length === 0) return data
  const g = range.bucket
  const first = Math.min(...data.map((b) => b.Start))
  const startSec = range.from > 0 ? Math.min(range.from, first) : first
  const starts: number[] = []
  for (let d = slotFloor(startSec, g); d.getTime() / 1000 < range.to; d = nextSlot(d, g)) {
    starts.push(Math.floor(d.getTime() / 1000))
    if (starts.length > MAX_SLOTS) return data
  }
  if (starts.length === 0) return data
  const out = starts.map((start) => ({ Start: start, Plays: 0, MsPlayed: 0 }))
  for (const b of data) {
    const mid = b.Start + 43_200
    let i = starts.length - 1
    while (i > 0 && starts[i] > mid) i--
    out[i].Plays += b.Plays
    out[i].MsPlayed += b.MsPlayed
  }
  return out
}

/** A round step (1, 2 or 5 × 10^n) of at least raw. */
function niceStep(raw: number): number {
  if (raw <= 0) return 1
  const magnitude = Math.pow(10, Math.floor(Math.log10(raw)))
  const n = raw / magnitude
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * magnitude
}

/** Infer bucket granularity by checking the smallest gap between starts. */
function inferGranularity(data: TimeBucket[]): 'day' | 'week' | 'month' {
  if (data.length < 2) return 'day'
  const gaps = data.slice(1).map((b, i) => b.Start - data[i].Start)
  const minGap = Math.min(...gaps)
  if (minGap < 7 * 86_400) return 'day'
  if (minGap < 28 * 86_400) return 'week'
  return 'month'
}

const MONTH_SHORT = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
  'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function formatBucketLabel(startSec: number, granularity: 'day' | 'week' | 'month'): string {
  const d = new Date(startSec * 1000)
  const month = MONTH_SHORT[d.getMonth()]
  if (granularity === 'month') return month
  return `${month} ${d.getDate()}`
}

function formatValue(value: number, metric: 'plays' | 'ms'): string {
  if (metric === 'plays') return `${value} play${value === 1 ? '' : 's'}`
  const mins = Math.round(value / 60_000)
  if (mins < 60) return `${mins}m`
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return m > 0 ? `${h}h ${m}m` : `${h}h`
}

/** Pick at most maxLabels evenly spaced indices from an array. */
function thinIndices(total: number, maxLabels: number): Set<number> {
  const result = new Set<number>()
  if (total === 0) return result
  result.add(0)
  result.add(total - 1)
  if (total <= maxLabels) {
    for (let i = 0; i < total; i++) result.add(i)
    return result
  }
  const step = Math.ceil(total / (maxLabels - 1))
  for (let i = 0; i < total; i += step) result.add(i)
  return result
}

// ── Chart constants ────────────────────────────────────────────────────────────

const VIEW_W = 600
const VIEW_H = 220
const MARGIN = { top: 16, right: 16, bottom: 36, left: 44 }
const CHART_W = VIEW_W - MARGIN.left - MARGIN.right
const CHART_H = VIEW_H - MARGIN.top - MARGIN.bottom
const Y_GRIDLINES = 4
const MAX_X_LABELS = 8
const BAR_GAP = 0.18 // fraction of bar+gap width reserved for gap

// ── Component ─────────────────────────────────────────────────────────────────

export function TimelineChart({ data: rawData, metric = 'plays', range }: Props) {
  const [tooltip, setTooltip] = useState<Tooltip | null>(null)
  const gradId = useId()
  const data = range ? fillBuckets(rawData, range) : rawData

  if (data.length === 0) {
    return (
      <div className="flex items-center justify-center h-40 rounded-lg bg-raised">
        <p className="text-sm text-text-muted">No listening data in this range</p>
      </div>
    )
  }

  const granularity = range?.bucket ?? inferGranularity(data)
  const values = data.map((b) => metric === 'plays' ? b.Plays : b.MsPlayed)
  const maxVal = Math.max(...values, 1)

  // Y axis: about Y_GRIDLINES round steps. Plays are whole numbers, so their
  // step is never below 1 (a max of 1 used to label the axis 0, 0, 1, 1, 1).
  const rawStep = maxVal / Y_GRIDLINES
  const gridStep = metric === 'plays' ? Math.max(1, niceStep(rawStep)) : niceStep(rawStep)
  const yAxisMax = gridStep * Math.ceil(maxVal / gridStep)

  // Bar layout
  const slotW = CHART_W / data.length
  const barW = slotW * (1 - BAR_GAP)

  const gridLines = Array.from({ length: Math.round(yAxisMax / gridStep) + 1 }, (_, i) => i * gridStep)

  const xLabelIndices = thinIndices(data.length, MAX_X_LABELS)

  // Map value to chart Y (SVG origin top-left)
  function toY(val: number): number {
    return CHART_H - (val / yAxisMax) * CHART_H
  }

  function formatYLabel(val: number): string {
    if (metric === 'ms') {
      const mins = Math.round(val / 60_000)
      if (mins === 0) return '0'
      if (mins < 60) return `${mins}m`
      return `${Math.floor(mins / 60)}h`
    }
    if (val >= 1_000) return `${(val / 1_000).toFixed(val % 1_000 === 0 ? 0 : 1)}k`
    return String(Math.round(val))
  }

  return (
    <div className="relative w-full select-none">
      <svg
        viewBox={`0 0 ${VIEW_W} ${VIEW_H}`}
        width="100%"
        aria-label={`Listening over time chart — ${metric === 'plays' ? 'plays' : 'minutes'}`}
        style={{ overflow: 'visible' }}
      >
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="rgb(var(--color-accent))" stopOpacity="0.8" />
            <stop offset="100%" stopColor="rgb(var(--color-accent))" stopOpacity="0.3" />
          </linearGradient>
        </defs>

        <g transform={`translate(${MARGIN.left},${MARGIN.top})`}>
          {/* Y gridlines + labels */}
          {gridLines.map((val) => {
            const y = toY(val)
            return (
              <g key={val}>
                <line
                  x1={0}
                  x2={CHART_W}
                  y1={y}
                  y2={y}
                  stroke="var(--border-subtle)"
                  strokeWidth={1}
                />
                <text
                  data-ylabel
                  x={-8}
                  y={y}
                  textAnchor="end"
                  dominantBaseline="middle"
                  fontSize={10}
                  fill="var(--text-muted)"
                >
                  {formatYLabel(val)}
                </text>
              </g>
            )
          })}

          {/* Bars */}
          {data.map((bucket, i) => {
            const val = values[i]
            const barH = Math.max((val / yAxisMax) * CHART_H, val > 0 ? 2 : 0)
            const x = i * slotW + (slotW - barW) / 2
            const y = CHART_H - barH

            return (
              <rect
                key={bucket.Start}
                data-bar
                x={x}
                y={y}
                width={barW}
                height={barH}
                fill={`url(#${gradId})`}
                rx={Math.min(3, barW / 4)}
                style={{ cursor: 'default' }}
                onMouseEnter={(e) => {
                  const svgEl = (e.currentTarget as SVGElement).closest('svg')!
                  const svgRect = svgEl.getBoundingClientRect()
                  const cx = svgRect.left + (MARGIN.left + x + barW / 2) / VIEW_W * svgRect.width
                  const cy = svgRect.top + (MARGIN.top + y) / VIEW_H * svgRect.height
                  setTooltip({
                    x: cx - svgRect.left,
                    y: cy - svgRect.top,
                    label: formatBucketLabel(bucket.Start, granularity),
                    value: formatValue(val, metric),
                    barX: x,
                    barWidth: barW,
                  })
                }}
                onMouseLeave={() => setTooltip(null)}
              />
            )
          })}

          {/* Baseline */}
          <line
            x1={0}
            x2={CHART_W}
            y1={CHART_H}
            y2={CHART_H}
            stroke="var(--border-subtle)"
            strokeWidth={1.5}
          />

          {/* X axis labels */}
          {data.map((bucket, i) => {
            if (!xLabelIndices.has(i)) return null
            const x = i * slotW + slotW / 2
            return (
              <text
                key={bucket.Start}
                x={x}
                y={CHART_H + 14}
                textAnchor="middle"
                fontSize={10}
                fill="var(--text-muted)"
              >
                {formatBucketLabel(bucket.Start, granularity)}
              </text>
            )
          })}
        </g>
      </svg>

      {/* Tooltip — positioned absolutely over the SVG */}
      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 px-2.5 py-1.5 rounded-md bg-raised shadow-float text-xs font-semibold text-text-primary whitespace-nowrap"
          style={{
            left: tooltip.x,
            top: tooltip.y - 36,
            transform: 'translateX(-50%)',
          }}
        >
          <span className="text-text-muted">{tooltip.label}</span>
          {' · '}
          {tooltip.value}
        </div>
      )}
    </div>
  )
}
