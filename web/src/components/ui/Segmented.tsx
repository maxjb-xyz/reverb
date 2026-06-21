
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
  return (
    <div
      role="tablist"
      className="inline-flex bg-raised rounded-full p-1 gap-0.5"
    >
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
              'px-4 py-1.5 rounded-full text-sm font-semibold transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              isSelected
                ? 'bg-accent text-on-accent'
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
