import { useLayoutEffect, useRef } from 'react'

interface SegmentedOption<T> {
  value: T
  label: string
}

interface SegmentedProps<T> {
  options: SegmentedOption<T>[]
  value: T
  onChange: (v: T) => void
}

export function Segmented<T extends string | number>({
  options,
  value,
  onChange,
}: SegmentedProps<T>) {
  const listRef = useRef<HTMLDivElement>(null)
  const pillRef = useRef<HTMLSpanElement>(null)

  // Slide one highlight under the selected tab. It is positioned straight on
  // the DOM; until it has been measured (and wherever there is no layout) the
  // selected tab paints its own background instead.
  useLayoutEffect(() => {
    const list = listRef.current
    const pill = pillRef.current
    if (!list || !pill) return
    function measure() {
      const tab = list!.querySelector<HTMLElement>('[aria-selected="true"]')
      if (!tab || tab.offsetWidth === 0) { delete list!.dataset.ready; return }
      const first = list!.dataset.ready !== 'true'
      if (first) pill!.style.transitionDuration = '0s'
      pill!.style.left = `${tab.offsetLeft}px`
      pill!.style.width = `${tab.offsetWidth}px`
      list!.dataset.ready = 'true'
      if (first) requestAnimationFrame(() => { pill!.style.transitionDuration = '' })
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(list)
    return () => ro.disconnect()
  }, [value, options.length])

  return (
    <div
      ref={listRef}
      role="tablist"
      className="group relative inline-flex bg-raised rounded-full p-1 gap-0.5"
    >
      <span
        ref={pillRef}
        aria-hidden
        className="absolute top-1 bottom-1 rounded-full bg-text-primary opacity-0 transition-[left,width] duration-300 ease-smooth group-data-[ready=true]:opacity-100"
      />
      {options.map((opt) => {
        const isSelected = opt.value === value
        return (
          <button
            key={String(opt.value)}
            role="tab"
            type="button"
            aria-selected={isSelected}
            onClick={() => onChange(opt.value)}
            className={[
              'rvb-press relative px-4 py-1.5 rounded-full text-sm font-semibold',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              isSelected
                ? 'bg-text-primary text-on-accent group-data-[ready=true]:bg-transparent'
                : 'text-text-secondary hover:text-text-primary',
            ].join(' ')}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
