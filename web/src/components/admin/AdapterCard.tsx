import { Badge, Button, Toggle } from '../ui'
import type { AdapterInstance } from '../../lib/adaptersApi'
import { SECRET_SENTINEL } from '../../lib/adaptersApi'

interface AdapterCardProps {
  instance: AdapterInstance
  onTest: (instance: AdapterInstance) => void
  onEdit: (instance: AdapterInstance) => void
  onToggle: (instance: AdapterInstance) => void
  onRemove: (id: string) => void
  onReorder?: (instance: AdapterInstance, delta: number) => void
  order?: number
  pendingRestart?: boolean
}

/**
 * Builds a one-line redacted config summary.
 * Values for keys ending in `__isSet` are sidecar booleans — skip them.
 * Values that equal SECRET_SENTINEL or whose sidecar `__isSet` is truthy
 * are displayed as SECRET_SENTINEL; everything else is shown as-is.
 */
function buildConfigSummary(config: Record<string, unknown>): string {
  const parts: string[] = []
  for (const key of Object.keys(config)) {
    if (key.endsWith('__isSet')) continue
    const isSet = Boolean(config[`${key}__isSet`])
    const value = config[key]
    if (isSet || value === SECRET_SENTINEL) {
      parts.push(`${key}: ${SECRET_SENTINEL}`)
    } else if (value !== undefined && value !== null && value !== '') {
      parts.push(`${key}: ${String(value)}`)
    }
  }
  return parts.join(' · ')
}

/** Derive status label and tone from enabled state and optional pending restart. */
function deriveStatus(
  enabled: boolean,
  pendingRestart: boolean
): { label: string; tone: 'success' | 'warning' | 'error' | undefined } {
  if (!enabled) return { label: 'Disabled', tone: undefined }
  if (pendingRestart) return { label: 'Restart pending', tone: 'warning' }
  return { label: 'Connected', tone: 'success' }
}

export function AdapterCard({
  instance,
  onTest,
  onEdit,
  onToggle,
  onRemove,
  onReorder,
  order,
  pendingRestart = false,
}: AdapterCardProps) {
  const { label: statusLabel, tone: statusTone } = deriveStatus(instance.enabled, pendingRestart)
  const configSummary = buildConfigSummary(instance.config)
  const logoLetter = instance.name.charAt(0).toUpperCase()

  return (
    <article className="flex items-center gap-4 rounded-lg border border-border-subtle bg-raised px-4 py-4">
      {order !== undefined && (
        <span className="w-6 flex-none text-center font-mono text-xs font-bold text-text-muted">
          {order}
        </span>
      )}

      {/* Logo chip */}
      <div
        aria-hidden="true"
        className="flex h-11 w-11 flex-none items-center justify-center rounded-lg bg-raised-hover text-lg font-black text-text-secondary"
      >
        {logoLetter}
      </div>

      {/* Meta */}
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-bold text-text-primary">{instance.name}</span>
          <Badge kind="status" tone={statusTone}>
            {statusLabel}
          </Badge>
        </div>
        {configSummary && (
          <p className="truncate font-mono text-xs text-text-muted">{configSummary}</p>
        )}
      </div>

      {/* Reorder controls (search/downloader) */}
      {onReorder && (
        <div className="flex flex-col gap-0.5">
          <button
            type="button"
            aria-label={`Move ${instance.name} up`}
            onClick={() => onReorder(instance, -1)}
            className="flex h-5 w-5 items-center justify-center rounded text-text-muted transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            &#8593;
          </button>
          <button
            type="button"
            aria-label={`Move ${instance.name} down`}
            onClick={() => onReorder(instance, 1)}
            className="flex h-5 w-5 items-center justify-center rounded text-text-muted transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            &#8595;
          </button>
        </div>
      )}

      {/* Controls */}
      <div className="flex flex-none items-center gap-2">
        <Toggle
          checked={instance.enabled}
          onChange={() => onToggle(instance)}
          label={`${instance.enabled ? 'Disable' : 'Enable'} ${instance.name}`}
        />
        <Button size="sm" variant="secondary" onClick={() => onTest(instance)} aria-label={`Test ${instance.name}`}>
          Test
        </Button>
        <Button size="sm" variant="secondary" onClick={() => onEdit(instance)} aria-label={`Edit ${instance.name}`}>
          Edit
        </Button>
        <Button size="sm" variant="ghost" onClick={() => onRemove(instance.id)} aria-label={`Remove ${instance.name}`}>
          Remove
        </Button>
      </div>
    </article>
  )
}
