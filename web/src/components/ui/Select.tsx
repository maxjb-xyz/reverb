
interface SelectOption {
  value: string
  label: string
}

interface SelectProps {
  value: string
  options: SelectOption[]
  onChange: (v: string) => void
  label: string
}

export function Select({ value, options, onChange, label }: SelectProps) {
  return (
    <div className="relative inline-flex items-center bg-input border border-border-subtle rounded-md">
      <select
        value={value}
        aria-label={label}
        onChange={(e) => onChange(e.target.value)}
        className={[
          'appearance-none bg-transparent px-3 py-2 pr-8 text-sm font-semibold text-text-primary',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded-md',
          'cursor-pointer',
        ].join(' ')}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      {/* chevron indicator */}
      <svg
        aria-hidden="true"
        viewBox="0 0 24 24"
        width="14"
        height="14"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        className="absolute right-2.5 pointer-events-none text-text-secondary"
      >
        <path d="M6 9l6 6 6-6" />
      </svg>
    </div>
  )
}
