import { useState } from 'react'
import { Button, EmptyState, Icon } from '../ui'
import { AdapterCard } from './AdapterCard'
import { AdapterForm } from '../AdapterForm'
import type { AdapterInstance, AvailableAdapter } from '../../lib/adaptersApi'

interface AdapterSectionProps {
  title: string
  subtitle?: string
  type: 'library' | 'search' | 'downloader'
  instances: AdapterInstance[]
  available: AvailableAdapter[]
  onCreate: (name: string, config: Record<string, unknown>) => Promise<void>
  onUpdate: (instance: AdapterInstance, config: Record<string, unknown>) => Promise<void>
  onToggle: (instance: AdapterInstance) => void
  onRemove: (id: string) => void
  onReorder?: (instance: AdapterInstance, delta: number) => void
  /** Downloader-only: swap the order value for granularity `g` between adjacent instances in a column. */
  onMoveInColumn?: (column: AdapterInstance[], index: number, direction: 'up' | 'down', g: string) => void
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

/**
 * Downloader chain order is controlled via the two-column granularity ordering
 * rendered below the management list. The priority reorder arrows are suppressed
 * for downloader type — granularity order is what matters.
 */
const REORDER_DISABLED_TYPES = new Set(['downloader'])

export function AdapterSection({
  title,
  subtitle,
  type,
  instances,
  available,
  onCreate,
  onUpdate,
  onToggle,
  onRemove,
  onReorder,
  onMoveInColumn,
}: AdapterSectionProps) {
  const ordered = sortInstances(type, instances)
  const showOrder = type === 'search' || type === 'downloader'
  // Downloaders are ordered via the two-column granularity ordering below the
  // management list. Suppress priority reorder arrows for downloader type.
  const reorderDisabled = REORDER_DISABLED_TYPES.has(type)

  // Downloader two-column ordering: enabled instances with granularities data.
  const enabledDownloaders =
    type === 'downloader'
      ? instances.filter((a) => a.enabled && a.granularities != null)
      : []
  const songDownloaders = enabledDownloaders
    .filter((a) => 'track' in (a.granularities ?? {}))
    .slice()
    .sort((a, b) => (a.granularities!['track'] ?? 0) - (b.granularities!['track'] ?? 0))
  const albumDownloaders = enabledDownloaders
    .filter((a) => 'album' in (a.granularities ?? {}))
    .slice()
    .sort((a, b) => (a.granularities!['album'] ?? 0) - (b.granularities!['album'] ?? 0))
  const anyGranularityDownloaders = songDownloaders.length > 0 || albumDownloaders.length > 0
  const emptyMessage = EMPTY_MESSAGES[type] ?? 'No providers configured'
  const typeLabel = TYPE_LABEL[type] ?? type
  const addLabel = `Add ${typeLabel}`

  // Inline editor state. `adding` opens the add flow (select provider → configure);
  // `editing` opens the configure form for an existing instance. Only one at a time.
  const [adding, setAdding] = useState(false)
  // The provider chosen in the add flow. With a single available provider we
  // auto-select it; with several the user picks one first.
  const [chosenName, setChosenName] = useState<string | null>(null)
  const [editing, setEditing] = useState<AdapterInstance | null>(null)

  function resetEditor() {
    setAdding(false)
    setChosenName(null)
    setEditing(null)
  }

  function startAdd() {
    setEditing(null)
    setChosenName(available.length === 1 ? available[0].name : null)
    setAdding(true)
  }

  function startEdit(inst: AdapterInstance) {
    setAdding(false)
    setChosenName(null)
    setEditing(inst)
  }

  const chosen = adding ? available.find((a) => a.name === chosenName) ?? null : null
  const editSchema =
    editing ? available.find((a) => a.name === editing.name)?.configSchema ?? { fields: [] } : null

  const editorOpen = adding || editing !== null

  return (
    <section className="space-y-3">
      {/* Section header */}
      <div className="flex items-center gap-3">
        <h2 className="text-lg font-extrabold tracking-tight text-text-primary">{title}</h2>
        <span className="text-xs font-bold text-text-muted">{instances.length}</span>
        {subtitle && <span className="text-xs text-text-muted">{subtitle}</span>}
        <div className="ml-auto">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => (editorOpen ? resetEditor() : startAdd())}
            aria-label={editorOpen ? 'Close' : addLabel}
            disabled={available.length === 0 && !editorOpen}
          >
            {editorOpen ? 'Close' : `+ ${addLabel}`}
          </Button>
        </div>
      </div>

      {/* Inline editor — provider selection then configuration (or edit) */}
      {editorOpen && (
        <div className="rounded-lg border border-border-subtle bg-raised p-4 animate-scale-in">
          {/* ADD: choose a provider when more than one is available */}
          {adding && !chosen && (
            <div className="space-y-3">
              <p className="text-sm font-bold text-text-primary">Choose a {typeLabel} provider</p>
              {available.length === 0 ? (
                <p className="text-sm text-text-muted">No {typeLabel} providers are available.</p>
              ) : (
                <div className="flex flex-wrap gap-2">
                  {available.map((a) => (
                    <button
                      key={a.name}
                      type="button"
                      onClick={() => setChosenName(a.name)}
                      className="rounded-full border border-border-subtle px-3 py-1.5 text-sm font-semibold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    >
                      {a.name}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* ADD (provider chosen) or EDIT: the configuration form */}
          {(chosen || editing) && (
            <div className="space-y-3">
              <div className="flex items-center gap-2">
                {adding && available.length > 1 && (
                  <button
                    type="button"
                    aria-label="Back to provider list"
                    onClick={() => setChosenName(null)}
                    className="text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
                  >
                    <Icon name="back" className="text-base" />
                  </button>
                )}
                <h3 className="text-sm font-extrabold text-text-primary">
                  {chosen ? `Add ${chosen.name}` : `Edit ${editing?.name}`}
                </h3>
              </div>
              <AdapterForm
                name={chosen ? chosen.name : editing!.name}
                schema={chosen ? chosen.configSchema : editSchema ?? { fields: [] }}
                initial={editing?.config}
                submitLabel={chosen ? 'Add' : 'Save'}
                supportedGranularities={editing?.supportedGranularities}
                granularities={editing?.granularities}
                priority={editing?.priority}
                onSubmit={async (config) => {
                  if (chosen) await onCreate(chosen.name, config)
                  else if (editing) await onUpdate(editing, config)
                  resetEditor()
                }}
              />
            </div>
          )}

          <button
            type="button"
            onClick={resetEditor}
            className="mt-3 text-sm text-text-muted hover:text-text-secondary transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
          >
            Cancel
          </button>
        </div>
      )}

      {/* Instances list or empty state */}
      {ordered.length === 0 ? (
        !editorOpen && (
          <EmptyState
            icon="search"
            title={emptyMessage}
            hint={`Click "${addLabel}" to configure your first ${typeLabel}.`}
          />
        )
      ) : (
        <ul className="space-y-2" role="list">
          {ordered.map((inst, idx) => (
            <li key={inst.id}>
              <AdapterCard
                instance={inst}
                order={showOrder ? idx + 1 : undefined}
                onTest={startEdit}
                onEdit={startEdit}
                onToggle={onToggle}
                onRemove={onRemove}
                onReorder={reorderDisabled ? undefined : onReorder}
              />
            </li>
          ))}
        </ul>
      )}

      {/* Downloader two-column ordering — Song | Album, each independently reorderable */}
      {type === 'downloader' && anyGranularityDownloaders && onMoveInColumn && (
        <div className="space-y-3 pt-2">
          <div>
            <h3 className="text-sm font-extrabold tracking-tight text-text-primary">Fallback order</h3>
            <p className="text-xs text-text-muted mt-0.5">
              Fallback chain per granularity — tried in order. Reorder each column independently.
            </p>
          </div>
          <div className="grid grid-cols-2 gap-4">
            {/* Song column */}
            <div data-testid="downloaders-song-col">
              <div className="text-xs font-bold uppercase tracking-widest text-text-secondary mb-2">
                Song
              </div>
              <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-surface">
                {songDownloaders.map((dl, i) => (
                  <div key={dl.id} className="flex items-center gap-3 px-3 py-2">
                    <div className="flex-1 min-w-0">
                      <span
                        data-testid="downloader-name"
                        className="text-sm font-semibold text-text-primary"
                      >
                        {dl.name}
                      </span>
                    </div>
                    <div className="flex gap-1">
                      <button
                        aria-label="Move up"
                        disabled={i === 0}
                        onClick={() => onMoveInColumn(songDownloaders, i, 'up', 'track')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8593;
                      </button>
                      <button
                        aria-label="Move down"
                        disabled={i === songDownloaders.length - 1}
                        onClick={() => onMoveInColumn(songDownloaders, i, 'down', 'track')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8595;
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>

            {/* Album column */}
            <div data-testid="downloaders-album-col">
              <div className="text-xs font-bold uppercase tracking-widest text-text-secondary mb-2">
                Album
              </div>
              <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-surface">
                {albumDownloaders.map((dl, i) => (
                  <div key={dl.id} className="flex items-center gap-3 px-3 py-2">
                    <div className="flex-1 min-w-0">
                      <span
                        data-testid="downloader-name"
                        className="text-sm font-semibold text-text-primary"
                      >
                        {dl.name}
                      </span>
                    </div>
                    <div className="flex gap-1">
                      <button
                        aria-label="Move up"
                        disabled={i === 0}
                        onClick={() => onMoveInColumn(albumDownloaders, i, 'up', 'album')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8593;
                      </button>
                      <button
                        aria-label="Move down"
                        disabled={i === albumDownloaders.length - 1}
                        onClick={() => onMoveInColumn(albumDownloaders, i, 'down', 'album')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8595;
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
