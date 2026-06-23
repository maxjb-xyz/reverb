interface EqualizerProps {
  className?: string
  playing?: boolean
}

// Animation delay classes for each bar, staggered per the mockup
// Mockup: bar1=0s, bar2=0.3s, bar3=0.15s, bar4=0.45s
const BAR_DELAYS = ['[animation-delay:0s]', '[animation-delay:.3s]', '[animation-delay:.15s]', '[animation-delay:.45s]']

export function Equalizer({ className, playing }: EqualizerProps) {
  return (
    <span
      className={['inline-flex items-end gap-px h-3.5', className]
        .filter(Boolean)
        .join(' ')}
    >
      {BAR_DELAYS.map((delay, i) => (
        <span
          key={i}
          data-testid="eq-bar"
          className={[
            'w-px h-1 bg-accent rounded-sm animate-eq',
            playing === false ? '[animation-play-state:paused]' : '',
            delay,
          ].filter(Boolean).join(' ')}
          style={{ animationDelay: ['0s', '.3s', '.15s', '.45s'][i] }}
        />
      ))}
    </span>
  )
}
