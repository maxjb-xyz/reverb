import { useEffect, useId, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Button } from './Button'

interface ConfirmDialogProps {
  open: boolean
  title: string
  /** Optional supporting line under the title. */
  body?: ReactNode
  confirmLabel: string
  cancelLabel?: string
  onConfirm: () => void
  onCancel: () => void
}

/** In-app replacement for window.confirm: same yes/no question, but styled
 *  like the rest of Reverb and with a button that names the action. */
export function ConfirmDialog({ open, title, body, confirmLabel, cancelLabel = 'Cancel', onConfirm, onCancel }: ConfirmDialogProps) {
  const titleId = useId()
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    panelRef.current?.querySelector<HTMLButtonElement>('button[data-confirm]')?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onCancel])

  if (!open) return null

  return createPortal(
    <>
      <div className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm animate-fade-in" aria-hidden="true" onClick={onCancel} />
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center p-4"
      >
        <div
          ref={panelRef}
          className="pointer-events-auto w-full max-w-sm space-y-4 rounded-xl border border-border-subtle bg-raised p-6 shadow-pop animate-scale-in"
        >
          <h2 id={titleId} className="text-lg font-extrabold tracking-tight text-text-primary">
            {title}
          </h2>
          {body ? <div className="text-sm text-text-secondary">{body}</div> : null}
          <div className="flex justify-end gap-2 pt-1">
            <Button variant="ghost" onClick={onCancel}>
              {cancelLabel}
            </Button>
            <ConfirmButton onClick={onConfirm}>{confirmLabel}</ConfirmButton>
          </div>
        </div>
      </div>
    </>,
    document.body,
  )
}

function ConfirmButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      data-confirm
      onClick={onClick}
      className="inline-flex items-center justify-center whitespace-nowrap rounded-full bg-accent px-4 py-2 text-sm font-semibold text-on-accent transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-raised"
    >
      {children}
    </button>
  )
}
