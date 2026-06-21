import React from 'react'
import { Icon } from './Icon'
import type { IconName } from './Icon'

interface EmptyStateProps {
  icon: IconName
  title: string
  hint?: string
  action?: React.ReactNode
}

export function EmptyState({ icon, title, hint, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-12 px-6 text-center">
      <div
        data-testid="empty-icon"
        className="text-text-muted text-4xl mb-1"
      >
        <Icon name={icon} className="w-12 h-12" />
      </div>
      <p className="text-text-primary font-bold text-base">{title}</p>
      {hint && (
        <p data-testid="empty-hint" className="text-text-muted text-sm max-w-xs">
          {hint}
        </p>
      )}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}
