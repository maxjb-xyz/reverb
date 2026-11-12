import React from 'react'

interface ChipProps {
  selected?: boolean
  onClick?: React.MouseEventHandler<HTMLButtonElement>
  children: React.ReactNode
  /** Render as a tab inside a role="tablist" container (sets aria-selected). */
  asTab?: boolean
  /** Render as a toggle (sets aria-pressed to `selected`). */
  asToggle?: boolean
}

export function Chip({ selected = false, onClick, children, asTab = false, asToggle = false }: ChipProps) {
  return (
    <button
      type="button"
      role={asTab ? 'tab' : undefined}
      aria-selected={asTab ? selected : undefined}
      aria-pressed={asToggle ? selected : undefined}
      onClick={onClick}
      className={[
        'inline-flex items-center px-3 py-1.5 rounded-full text-sm font-semibold',
        'transition-colors cursor-pointer whitespace-nowrap',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        selected
          ? 'bg-text-primary text-on-accent'
          : 'bg-raised text-text-primary hover:bg-raised-hover',
      ].join(' ')}
    >
      {children}
    </button>
  )
}
