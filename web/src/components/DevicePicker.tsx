import { useEffect, useRef, useState } from 'react'
import { Icon } from './ui/Icon'
import { usePlayer } from '../lib/playerStore'
import { transferTo, useRemoteDeviceName, useSession, type Device } from '../lib/playbackSession'

// The device picker: which of the account's devices plays the music. Picking
// one moves the queue there and carries on from the same spot.

const KIND_LABEL: Record<Device['kind'], string> = {
  web: 'Browser',
  desktop: 'Computer',
  phone: 'Phone',
  discord: 'Discord',
}

export function DevicePicker({ look = 'bar' }: { look?: 'bar' | 'mini' }) {
  const connected = useSession((s) => s.connected)
  const devices = useSession((s) => s.devices)
  const me = useSession((s) => s.deviceId)
  const activeId = useSession((s) => s.activeDeviceId)
  const remoteName = useRemoteDeviceName()
  const playing = usePlayer((s) => s.playing)
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!connected) return null
  const mini = look === 'mini'
  // This tab first, then the rest as the server ordered them.
  const others = devices.filter((d) => d.id !== me)
  // The phone bar is tight: only offer the picker when there is a choice.
  if (mini && others.length === 0 && !remoteName) return null

  function pick(id: string) {
    setOpen(false)
    // The device playing already: nothing to move.
    if (id === activeId || (id === me && !remoteName)) return
    transferTo(id, playing || !activeId)
  }

  return (
    <div ref={root} className={mini ? 'flex-none' : 'relative flex flex-none items-center gap-1.5'}>
      {!mini && remoteName && (
        <span data-testid="remote-device-label" className="max-w-[9rem] truncate text-xs font-semibold text-accent">
          {remoteName}
        </span>
      )}
      <button
        type="button"
        aria-label="Devices"
        aria-expanded={open}
        title={remoteName ? `Playing on ${remoteName}` : 'Connect to a device'}
        data-testid="device-button"
        onClick={() => setOpen((o) => !o)}
        className={[
          'rvb-press inline-grid place-items-center rounded-full transition-colors',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          mini ? 'h-11 w-11' : 'h-8 w-8',
          remoteName ? 'text-accent' : 'text-text-secondary hover:text-text-primary hover:bg-white/[.06]',
        ].join(' ')}
      >
        <Icon name="device" className={mini ? 'h-5 w-5' : 'h-4 w-4'} />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Devices"
          data-testid="device-panel"
          className={[
            'absolute bottom-full z-40 mb-3 rounded-xl border border-white/[.06] bg-raised p-2 shadow-pop animate-toast-in',
            mini ? 'inset-x-2' : 'right-0 w-72',
          ].join(' ')}
        >
          <p className="px-2 pb-1 pt-1 text-[11px] font-semibold uppercase tracking-wider text-text-muted">
            {remoteName ? 'Playing on' : 'Play on'}
          </p>
          <ul className="flex flex-col">
            <DeviceRow
              label="This browser"
              sub={devices.find((d) => d.id === me)?.name ?? ''}
              kind={devices.find((d) => d.id === me)?.kind ?? 'web'}
              current={!remoteName}
              onPick={() => pick(me)}
            />
            {others.map((d) => (
              <DeviceRow
                key={d.id}
                label={d.name}
                sub={KIND_LABEL[d.kind] ?? 'Device'}
                kind={d.kind}
                current={d.id === activeId}
                onPick={() => pick(d.id)}
              />
            ))}
          </ul>
          {others.length === 0 && (
            <p className="px-2 pb-1 pt-2 text-xs leading-relaxed text-text-muted">
              Open Reverb on another device signed in to this account and it shows up here.
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function DeviceRow({
  label,
  sub,
  kind,
  current,
  onPick,
}: {
  label: string
  sub: string
  kind: Device['kind']
  current: boolean
  onPick: () => void
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onPick}
        aria-current={current ? 'true' : undefined}
        data-testid={`device-${kind}`}
        className={[
          'flex w-full items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-white/[.06]',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        ].join(' ')}
      >
        <span
          className={[
            'inline-grid h-8 w-8 flex-none place-items-center rounded-full',
            current ? 'bg-accent text-on-accent' : 'bg-white/[.06] text-text-secondary',
          ].join(' ')}
        >
          <Icon name="device" className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className={['block truncate text-sm font-medium', current ? 'text-accent' : 'text-text-primary'].join(' ')}>
            {label}
          </span>
          {sub && <span className="block truncate text-xs text-text-muted">{sub}</span>}
        </span>
      </button>
    </li>
  )
}
