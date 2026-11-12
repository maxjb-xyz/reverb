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
  className?: string
}

export function IconButton({
  name,
  label,
  active = false,
  disabled = false,
  onClick,
  size = 'md',
  className,
}: IconButtonProps) {
  const sizeClass = size === 'sm' ? 'w-7 h-7 text-base' : 'w-8 h-8 text-lg'

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
      className={[
        'rvb-press inline-grid place-items-center rounded-full hover:bg-white/[.06]',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        active ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
        sizeClass,
        className,
      ].filter(Boolean).join(' ')}
    >
      <Icon name={name} />
    </button>
  )
}
