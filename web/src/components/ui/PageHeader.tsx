import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: ReactNode
  /** One quiet line under the title (counts, context). */
  subtitle?: ReactNode
  /** Right-aligned actions; wraps under the title on narrow screens. */
  actions?: ReactNode
}

/**
 * The one page-title treatment for every top-level page, so headings share a
 * size, weight and rhythm instead of drifting per route.
 */
export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div className="min-w-0">
        <h1 className="truncate text-2xl font-extrabold tracking-tight text-text-primary">{title}</h1>
        {subtitle ? <p className="mt-1 text-sm text-text-secondary">{subtitle}</p> : null}
      </div>
      {actions ? <div className="flex flex-none items-center gap-2">{actions}</div> : null}
    </header>
  )
}
