import React from 'react'

interface ChipProps {
  selected?: boolean
  onClick?: React.MouseEventHandler<HTMLButtonElement>
  children: React.ReactNode
}

export function Chip({ selected = false, onClick, children }: ChipProps) {
  return (
    <button
      type="button"
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
