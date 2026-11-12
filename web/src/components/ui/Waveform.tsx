import { useMemo } from 'react'
import { resamplePeaks } from '../../lib/waveform'

interface WaveformProps {
  peaks: number[]
  /** Played fraction, 0–100. */
  pct: number
  /** Number of bars drawn; the raw peaks are resampled to this. */
  bars?: number
  className?: string
}

/** Scrubber waveform: slim rounded bars with visible gaps. Drawing one bar per
 *  raw peak packed them edge to edge, which read as a solid block. */
export function Waveform({ peaks, pct, bars = 96, className = 'h-6' }: WaveformProps) {
  const sampled = useMemo(() => resamplePeaks(peaks, bars), [peaks, bars])
  return (
    <div
      data-testid="waveform"
      className={`pointer-events-none absolute inset-x-0 top-1/2 flex -translate-y-1/2 items-center gap-[2px] ${className}`}
    >
      {sampled.map((peak, i) => (
        <div
          key={i}
          data-played={(i / sampled.length) * 100 <= pct ? 'true' : undefined}
          className={[
            'min-w-0 flex-1 rounded-full',
            (i / sampled.length) * 100 <= pct ? 'bg-text-primary group-hover:bg-accent' : 'bg-text-muted opacity-50',
          ].join(' ')}
          style={{ minHeight: '2px', height: `${Math.max(10, peak * 100)}%` }}
        />
      ))}
    </div>
  )
}
