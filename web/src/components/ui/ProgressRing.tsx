interface ProgressRingProps {
  value: number // 0-100
  size?: number
  indeterminate?: boolean
}

const RADIUS = 15
const CIRCUMFERENCE = 2 * Math.PI * RADIUS // ≈ 94.25

// Partial arc for indeterminate state: ~75% of circumference
const INDETERMINATE_DASH = CIRCUMFERENCE * 0.75
const INDETERMINATE_OFFSET = CIRCUMFERENCE * 0.25

export function ProgressRing({ value, size = 36, indeterminate = false }: ProgressRingProps) {
  const clampedValue = Math.min(100, Math.max(0, value))
  const dashoffset = CIRCUMFERENCE * (1 - clampedValue / 100)
  // The SVG viewBox is 0 0 36 36 with center at 18,18 and r=15
  const cx = 18
  const cy = 18

  if (indeterminate) {
    return (
      // Spin the whole SVG around its box center (the visual center of the ring)
      // so the arc rotates IN PLACE. Spinning the inner <circle> instead needs a
      // transform-origin that fights the SVG transform attribute and makes the arc
      // orbit/translate ("rise up from below"). Motion respects prefers-reduced-motion.
      <svg
        width={size}
        height={size}
        viewBox="0 0 36 36"
        aria-label="Loading"
        role="img"
        aria-busy="true"
        className="motion-safe:animate-spin"
      >
        {/* Track ring */}
        <circle
          cx={cx}
          cy={cy}
          r={RADIUS}
          fill="none"
          stroke="currentColor"
          strokeWidth={3}
          className="text-raised"
        />
        {/* Partial arc (static rotation positions its start; the SVG does the spinning) */}
        <circle
          cx={cx}
          cy={cy}
          r={RADIUS}
          fill="none"
          stroke="currentColor"
          strokeWidth={3}
          strokeLinecap="round"
          strokeDasharray={`${INDETERMINATE_DASH} ${INDETERMINATE_OFFSET}`}
          transform={`rotate(-90 ${cx} ${cy})`}
          className="text-accent"
        />
      </svg>
    )
  }

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 36 36"
      aria-label={`${clampedValue}% complete`}
      role="img"
    >
      {/* Track ring */}
      <circle
        cx={cx}
        cy={cy}
        r={RADIUS}
        fill="none"
        stroke="currentColor"
        strokeWidth={3}
        className="text-raised"
      />
      {/* Progress arc */}
      <circle
        cx={cx}
        cy={cy}
        r={RADIUS}
        fill="none"
        stroke="currentColor"
        strokeWidth={3}
        strokeLinecap="round"
        strokeDasharray={CIRCUMFERENCE}
        strokeDashoffset={dashoffset}
        transform={`rotate(-90 ${cx} ${cy})`}
        className="text-accent"
      />
    </svg>
  )
}
