import { Button, IconButton } from '../ui'
import { Icon } from '../ui/Icon'
import type { EngineSeed } from '../../lib/engineApi'
import { useStartRadio } from './useStartRadio'

interface Props {
  seed: EngineSeed
  label?: string
  /** 'button' is a labelled secondary button (artist page); 'icon' is a small icon button (player). */
  look?: 'button' | 'icon'
  className?: string
}

/** Starts a radio station from a track, artist or genre. */
export function RadioButton({ seed, label = 'Radio', look = 'button', className }: Props) {
  const { start, busy } = useStartRadio()
  if (look === 'icon') {
    return <IconButton name="radio" label={label} size="sm" disabled={busy} onClick={() => void start(seed)} className={className} />
  }
  return (
    <Button variant="secondary" size="md" disabled={busy} onClick={() => void start(seed)} className={['gap-2', className ?? ''].join(' ')}>
      <Icon name="radio" className="w-4 h-4" />
      {busy ? 'Starting…' : label}
    </Button>
  )
}
