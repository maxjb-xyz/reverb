import React from 'react'

interface BadgeProps {
  kind: 'in-library' | 'available' | 'downloading' | 'downloaded' | 'disabled' | 'status'
  tone?: 'success' | 'warning' | 'error'
  children: React.ReactNode
}

const kindClasses: Record<string, string> = {
  'in-library': 'text-accent',
  available: 'border border-border-subtle text-text-primary',
  downloading: 'text-accent',
  downloaded: 'text-accent',
  disabled: 'text-text-muted border border-border-subtle',
}

const toneClasses: Record<NonNullable<BadgeProps['tone']>, string> = {
  success: 'bg-success/10 text-success',
  warning: 'bg-warning/10 text-warning',
  error: 'bg-error/10 text-error',
}

export function Badge({ kind, tone, children }: BadgeProps) {
  const isStatus = kind === 'status'

  const colorClass = isStatus
    ? (tone ? toneClasses[tone] : 'text-text-secondary')
    : kindClasses[kind]

  return (
    <span
      className={[
        'inline-flex items-center gap-1.5 text-xs font-bold px-2.5 py-1 rounded-full',
        colorClass,
      ].join(' ')}
    >
      {isStatus && (
        <span
          data-testid="status-dot"
          className="w-1.5 h-1.5 rounded-full bg-current flex-none"
        />
      )}
      {children}
    </span>
  )
}
