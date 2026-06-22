import type { ReactNode } from 'react'

interface CarouselProps {
  title: string
  onShowAll?: () => void
  children: ReactNode
}

export function Carousel({ title, onShowAll, children }: CarouselProps) {
  return (
    <section>
      {/* Section header */}
      <div className="flex items-baseline mb-4">
        <h2 className="text-2xl font-extrabold tracking-tight text-text-primary cursor-default">
          {title}
        </h2>
        {onShowAll && (
          <button
            type="button"
            onClick={onShowAll}
            className={[
              'ml-auto text-sm font-bold text-text-muted',
              'hover:underline',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
            ].join(' ')}
          >
            Show all
          </button>
        )}
      </div>

      {/* Horizontal scroll row */}
      <div
        data-testid="carousel-scroll"
        className="grid grid-flow-col gap-4 overflow-x-auto pb-2 w-full min-w-0"
        style={{ gridAutoColumns: '160px' }}
      >
        {children}
      </div>
    </section>
  )
}
