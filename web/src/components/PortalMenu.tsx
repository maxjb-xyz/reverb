import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

interface PortalMenuProps {
  /** The trigger element whose bounding rect anchors the menu. */
  triggerRef: React.RefObject<HTMLElement | null>
  onClose: () => void
  /** aria-label for the menu panel. */
  label: string
  children: ReactNode
  /** Width class for the panel, e.g. "w-48" or "w-72". Defaults to "w-48". */
  widthClass?: string
}

/**
 * PortalMenu — renders a dropdown panel via a React portal to document.body so
 * it escapes any `overflow-auto` scroll-container ancestor (e.g. AppShell's
 * <main>). Position is computed from the trigger's `getBoundingClientRect()`
 * on mount; the menu closes on Esc or a backdrop click.
 */
export function PortalMenu({
  triggerRef,
  onClose,
  label,
  children,
  widthClass = 'w-48',
}: PortalMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null)

  // Compute fixed position from trigger rect on mount
  const rect = triggerRef.current?.getBoundingClientRect()
  const top = rect ? rect.bottom + 4 : 0
  // Align the right edge of the menu with the right edge of the trigger
  const right = rect ? window.innerWidth - rect.right : 0

  // Close on Esc
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  // Close on scroll/resize so the menu doesn't drift
  useEffect(() => {
    function handleScrollResize() {
      onClose()
    }
    window.addEventListener('scroll', handleScrollResize, { capture: true, passive: true })
    window.addEventListener('resize', handleScrollResize, { passive: true })
    return () => {
      window.removeEventListener('scroll', handleScrollResize, { capture: true })
      window.removeEventListener('resize', handleScrollResize)
    }
  }, [onClose])

  return createPortal(
    <>
      {/* Backdrop — click closes */}
      <div
        className="fixed inset-0 z-40"
        aria-hidden="true"
        onClick={onClose}
      />
      {/* Menu panel */}
      <div
        ref={menuRef}
        role="menu"
        aria-label={label}
        style={{ top, right }}
        className={`fixed z-50 ${widthClass} rounded-xl border border-border-subtle bg-raised shadow-pop`}
      >
        {children}
      </div>
    </>,
    document.body,
  )
}
