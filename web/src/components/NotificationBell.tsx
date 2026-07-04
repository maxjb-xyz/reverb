import { useRef, useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { createPortal } from 'react-dom'
import { useNotificationStore, postMarkRead } from '../lib/notificationApi'
import { IconButton } from './ui'

/**
 * NotificationBell — bell icon in the TopBar with unread-count badge and a
 * dropdown notification center. Uses the same portal + backdrop + Esc pattern
 * as PortalMenu / AddToPlaylistMenu.
 */
export function NotificationBell() {
  const navigate = useNavigate()
  const triggerRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; right: number } | null>(null)

  const unread = useNotificationStore((s) => s.unread)
  const items = useNotificationStore((s) => s.items)

  const notifications = items()

  // Badge display — cap at 9+
  const badgeLabel = unread > 9 ? '9+' : String(unread)

  const ariaLabel =
    unread > 0 ? `Notifications (${unread} unread)` : 'Notifications'

  function close() {
    setOpen(false)
    setPos(null)
  }

  // Toggle the dropdown. When opening, measure the trigger and position the
  // portal here in the event handler — reading the ref outside render (and never
  // calling setState inside an effect body).
  function toggle() {
    if (open) {
      close()
      return
    }
    const rect = triggerRef.current?.getBoundingClientRect()
    setPos(
      rect
        ? { top: rect.bottom + 4, right: window.innerWidth - rect.right }
        : { top: 0, right: 0 },
    )
    setOpen(true)
  }

  // Close on Escape (mirrors PortalMenu behavior)
  useEffect(() => {
    if (!open) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        close()
      }
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [open])

  async function handleNotificationClick(id: string) {
    close()
    useNotificationStore.getState().markRead([id])
    void postMarkRead([id])
    navigate('/requests')
  }

  async function handleMarkAllRead() {
    useNotificationStore.getState().markAllRead()
    void postMarkRead(undefined)
  }

  return (
    <div ref={triggerRef} className="relative">
      {/* Bell button */}
      <IconButton
        name="bell"
        label={ariaLabel}
        onClick={toggle}
      />

      {/* Unread count badge */}
      {unread > 0 && (
        <span
          data-testid="notification-badge"
          className="absolute -top-1 -right-1 min-w-4 h-4 px-1 rounded-full bg-accent text-on-accent text-xs font-extrabold grid place-items-center pointer-events-none"
        >
          {badgeLabel}
        </span>
      )}

      {/* Dropdown notification center — portaled to body */}
      {open &&
        pos &&
        createPortal(
          <>
            {/* Backdrop */}
            <div
              className="fixed inset-0 z-40"
              aria-hidden="true"
              onClick={close}
            />

            {/* Panel */}
            <div
              role="menu"
              aria-label="Notifications"
              style={{ top: pos.top, right: pos.right }}
              className="fixed z-50 w-80 rounded-xl border border-border-subtle bg-raised shadow-pop"
            >
              {/* Header */}
              <div className="flex items-center justify-between px-4 py-3 border-b border-border-subtle">
                <span className="text-sm font-bold text-text-primary">
                  Notifications
                </span>
                {unread > 0 && (
                  <button
                    type="button"
                    onClick={() => void handleMarkAllRead()}
                    className={[
                      'text-xs text-text-secondary hover:text-text-primary transition-colors',
                      'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded',
                    ].join(' ')}
                  >
                    Mark all read
                  </button>
                )}
              </div>

              {/* Notification list */}
              <ul
                className="max-h-96 overflow-y-auto py-1"
                role="list"
              >
                {notifications.length === 0 ? (
                  <li className="px-4 py-6 text-center text-sm text-text-muted">
                    No notifications yet
                  </li>
                ) : (
                  notifications.map((n) => (
                    <li key={n.id}>
                      <button
                        type="button"
                        aria-label={n.title}
                        onClick={() => void handleNotificationClick(n.id)}
                        className={[
                          'w-full text-left flex items-start gap-3 px-4 py-3',
                          'hover:bg-raised-hover transition-colors',
                          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-inset',
                        ].join(' ')}
                      >
                        {/* Unread dot */}
                        <span
                          className={[
                            'mt-1.5 w-2 h-2 rounded-full flex-none transition-opacity',
                            n.read ? 'opacity-0' : 'bg-accent',
                          ].join(' ')}
                          aria-hidden="true"
                        />

                        <span className="min-w-0 flex-1">
                          <span
                            className={[
                              'block text-sm truncate',
                              n.read ? 'font-medium text-text-secondary' : 'font-semibold text-text-primary',
                            ].join(' ')}
                          >
                            {n.title}
                          </span>
                          <span className="block text-xs text-text-muted leading-snug mt-0.5">
                            {n.body}
                          </span>
                          <span className="block text-xs text-text-muted mt-1">
                            {relativeTime(n.createdAt)}
                          </span>
                        </span>
                      </button>
                    </li>
                  ))
                )}
              </ul>
            </div>
          </>,
          document.body,
        )}
    </div>
  )
}

/** Formats a Unix-seconds timestamp as a human-friendly relative string. */
function relativeTime(ts: number): string {
  const diffSec = Math.floor(Date.now() / 1000 - ts)
  if (diffSec < 60) return 'just now'
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  return `${diffDay}d ago`
}
