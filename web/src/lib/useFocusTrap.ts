import { useEffect } from 'react'
import type { RefObject } from 'react'

const FOCUSABLE = 'button, [href], input, [tabindex]:not([tabindex="-1"])'

/**
 * useFocusTrap — traps Tab focus within `panelRef`, focuses the first focusable
 * element on open, closes on Escape, and restores focus to the previously
 * focused element on unmount/close. Mirrors the inline pattern already used by
 * ImportPlaylistDialog and AddToPlaylistMenu.
 *
 * @param active   whether the trap is engaged (e.g. the dialog is open)
 * @param panelRef ref to the dialog panel element
 * @param onClose  called when Escape is pressed
 */
export function useFocusTrap(
  active: boolean,
  panelRef: RefObject<HTMLElement | null>,
  onClose: () => void,
): void {
  useEffect(() => {
    if (!active) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    const panel = panelRef.current
    if (panel) {
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE))
      focusable[0]?.focus()
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (e.key === 'Tab' && panelRef.current) {
        const focusable = Array.from(
          panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter((el) => !el.hasAttribute('disabled'))
        if (focusable.length === 0) return
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }

    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('keydown', handleKey)
      previouslyFocused?.focus()
    }
  }, [active, panelRef, onClose])
}
