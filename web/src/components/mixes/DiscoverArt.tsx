import { Icon } from '../ui'

/**
 * The art for Discover Weekly: no single album cover fits a week of new
 * music. With no size it fills its box (for cards).
 */
export function DiscoverArt({ size }: { size?: number }) {
  return (
    <div
      aria-hidden="true"
      data-testid="discover-art"
      style={size ? { width: size, height: size } : undefined}
      className={[
        'flex flex-none flex-col justify-between rounded-md bg-gradient-to-br from-accent to-raised shadow-cover',
        size ? 'p-4' : 'aspect-square w-full p-3',
      ].join(' ')}
    >
      <Icon name="compass" className={size ? 'h-8 w-8 text-text-primary' : 'h-6 w-6 text-text-primary'} />
      <span className={['font-black leading-none tracking-tight text-text-primary', size ? 'text-2xl' : 'text-lg'].join(' ')}>
        Discover
        <br />
        Weekly.
      </span>
    </div>
  )
}
