import { Button, EmptyState } from '../ui'
import { AdapterCard } from './AdapterCard'
import type { AdapterInstance, AvailableAdapter } from '../../lib/adaptersApi'

interface AdapterSectionProps {
  title: string
  subtitle?: string
  type: 'library' | 'search' | 'downloader'
  instances: AdapterInstance[]
  available: AvailableAdapter[]
  onAdd: () => void
  onTest: (instance: AdapterInstance) => void
  onEdit: (instance: AdapterInstance) => void
  onToggle: (instance: AdapterInstance) => void
  onRemove: (id: string) => void
  onReorder?: (instance: AdapterInstance, delta: number) => void
  pendingRestart?: boolean
}

const EMPTY_MESSAGES: Record<string, string> = {
  library: 'No library providers configured',
  search: 'No search providers configured',
  downloader: 'No downloaders configured',
}

const TYPE_LABEL: Record<string, string> = {
  library: 'library',
  search: 'search',
  downloader: 'downloader',
}

/** Search and downloader sections order by priority; library shows insertion order. */
function sortInstances(type: string, instances: AdapterInstance[]): AdapterInstance[] {
  if (type === 'search' || type === 'downloader') {
    return [...instances].sort((a, b) => a.priority - b.priority)
  }
  return instances
}

export function AdapterSection({
  title,
  subtitle,
  type,
  instances,
  available: _available,
  onAdd,
  onTest,
  onEdit,
  onToggle,
  onRemove,
  onReorder,
  pendingRestart = false,
}: AdapterSectionProps) {
  const ordered = sortInstances(type, instances)
  const showOrder = type === 'search' || type === 'downloader'
  const emptyMessage = EMPTY_MESSAGES[type] ?? 'No providers configured'
  const addLabel = `Add ${TYPE_LABEL[type] ?? type}`

  return (
    <section className="space-y-3">
      {/* Section header */}
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">{title}</h2>
        <span className="text-xs font-bold text-text-muted">{instances.length}</span>
        {subtitle && (
          <span className="text-xs text-text-muted">{subtitle}</span>
        )}
        <div className="ml-auto">
          <Button
            size="sm"
            variant="secondary"
            onClick={onAdd}
            aria-label={addLabel}
          >
            + {addLabel}
          </Button>
        </div>
      </div>

      {/* Instances list or empty state */}
      {ordered.length === 0 ? (
        <EmptyState
          icon="search"
          title={emptyMessage}
          hint={`Click "${addLabel}" to configure your first ${TYPE_LABEL[type] ?? type}.`}
        />
      ) : (
        <ul className="space-y-2" role="list">
          {ordered.map((inst, idx) => (
            <li key={inst.id}>
              <AdapterCard
                instance={inst}
                order={showOrder ? idx + 1 : undefined}
                onTest={onTest}
                onEdit={onEdit}
                onToggle={onToggle}
                onRemove={onRemove}
                onReorder={onReorder}
                pendingRestart={pendingRestart}
              />
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
