
interface ToggleProps {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}

export function Toggle({ checked, onChange, label }: ToggleProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={[
        'relative inline-flex w-11 h-6 rounded-full transition-colors duration-200 flex-none',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        checked ? 'bg-accent' : 'bg-raised-hover',
      ].join(' ')}
    >
      <span
        className={[
          'absolute top-[3px] w-[18px] h-[18px] rounded-full bg-on-accent shadow-sm transition-transform duration-300 ease-spring',
          checked ? 'translate-x-[22px]' : 'translate-x-[3px]',
        ].join(' ')}
      />
    </button>
  )
}
