interface ProgressRingProps {
  value: number // 0-100
  size?: number
}

const RADIUS = 15
const CIRCUMFERENCE = 2 * Math.PI * RADIUS // ≈ 94.25

export function ProgressRing({ value, size = 36 }: ProgressRingProps) {
  const clampedValue = Math.min(100, Math.max(0, value))
  const dashoffset = CIRCUMFERENCE * (1 - clampedValue / 100)
  // The SVG viewBox is 0 0 36 36 with center at 18,18 and r=15
  const cx = 18
  const cy = 18

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
