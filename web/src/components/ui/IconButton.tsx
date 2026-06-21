import React from 'react'
import { Icon } from './Icon'
import type { IconName } from './Icon'

interface IconButtonProps {
  name: IconName
  label: string
  active?: boolean
  disabled?: boolean
  onClick?: React.MouseEventHandler<HTMLButtonElement>
  size?: 'sm' | 'md'
}

export function IconButton({
  name,
  label,
  active = false,
  disabled = false,
  onClick,
  size = 'md',
}: IconButtonProps) {
  const sizeClass = size === 'sm' ? 'w-7 h-7 text-base' : 'w-8 h-8 text-lg'

  return (
    <button
      type="button"
      aria-label={label}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
      className={[
        'inline-grid place-items-center rounded-full transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        active ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
        sizeClass,
      ].join(' ')}
    >
      <Icon name={name} />
    </button>
  )
}
