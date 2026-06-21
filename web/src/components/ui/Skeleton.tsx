interface SkeletonProps {
  className?: string
  rounded?: 'md' | 'full'
  'data-testid'?: string
}

export function Skeleton({ className, rounded = 'md', 'data-testid': testId }: SkeletonProps) {
  const roundedClass = rounded === 'full' ? 'rounded-full' : 'rounded-md'

  return (
    <div
      data-testid={testId}
      className={[
        'bg-raised animate-pulse',
        roundedClass,
        className,
      ]
        .filter(Boolean)
        .join(' ')}
    />
  )
}
