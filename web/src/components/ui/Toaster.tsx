import { useToastStore } from '../../lib/toastStore'

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts)
  const dismiss = useToastStore((s) => s.dismiss)

  // Always render the aria-live region so it is in the DOM before the first
  // toast arrives — a live region must exist before content is injected for
  // screen readers to announce changes. When toasts is empty, the container
  // renders with no children (an empty polite live region is harmless).
  return (
    <div
      aria-live="polite"
      aria-label="Notifications"
      className="fixed bottom-6 right-6 z-50 flex flex-col gap-2 pointer-events-none"
    >
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role="status"
          data-testid="toast"
          className={[
            'flex items-center gap-3 px-4 py-3 rounded-lg shadow-pop border border-border-subtle',
            'bg-raised pointer-events-auto text-sm font-medium',
            toast.kind === 'success' ? 'text-success' : '',
            toast.kind === 'error' ? 'text-error' : '',
            toast.kind === 'info' ? 'text-text-primary' : '',
          ]
            .filter(Boolean)
            .join(' ')}
        >
          <span className="flex-1">{toast.message}</span>
          <button
            type="button"
            aria-label="Dismiss"
            onClick={() => dismiss(toast.id)}
            className="text-text-muted hover:text-text-primary transition-colors flex-none"
          >
            ✕
          </button>
        </div>
      ))}
    </div>
  )
}
