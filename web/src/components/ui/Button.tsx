import React from 'react'

interface ButtonProps {
  variant?: 'primary' | 'secondary' | 'ghost'
  size?: 'sm' | 'md'
  disabled?: boolean
  onClick?: React.MouseEventHandler<HTMLButtonElement>
  type?: 'button' | 'submit' | 'reset'
  'aria-label'?: string
  children: React.ReactNode
}

const variantClasses: Record<NonNullable<ButtonProps['variant']>, string> = {
  primary: 'bg-accent text-black hover:opacity-90',
  secondary: 'border border-border-subtle text-text-primary bg-transparent hover:bg-raised-hover',
  ghost: 'text-text-secondary bg-transparent hover:text-text-primary',
}

const sizeClasses: Record<NonNullable<ButtonProps['size']>, string> = {
  sm: 'px-3 py-1.5 text-sm font-semibold',
  md: 'px-4 py-2 text-sm font-semibold',
}

export function Button({
  variant = 'secondary',
  size = 'md',
  disabled = false,
  onClick,
  type = 'button',
  'aria-label': ariaLabel,
  children,
}: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled}
      aria-label={ariaLabel}
      onClick={disabled ? undefined : onClick}
      className={[
        'inline-flex items-center justify-center rounded-full transition-opacity',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        'disabled:opacity-40 disabled:cursor-not-allowed',
        variantClasses[variant],
        sizeClasses[size],
      ].join(' ')}
    >
      {children}
    </button>
  )
}
