import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useAdapters,
  useAvailableAdapters,
  usePendingRestart,
  createAdapter,
  updateAdapter,
  deleteAdapter,
  type AdapterInstance,
  type AvailableAdapter,
} from '../lib/adaptersApi'
import { AdapterSection } from '../components/admin/AdapterSection'
import { RestartBanner } from '../components/admin/RestartBanner'
import { AdapterForm } from '../components/AdapterForm'
import { Chip, Skeleton, EmptyState } from '../components/ui'

type Tab = 'providers' | 'server' | 'users'

/** Removes the `<key>__isSet` sidecar booleans before re-sending a redacted config. */
function stripIsSet(config: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const k of Object.keys(config)) {
    if (k.endsWith('__isSet')) continue
    out[k] = config[k]
  }
  return out
}

interface EditingState {
  section: string
  instance?: AdapterInstance
  add?: AvailableAdapter
}

export default function Admin() {
  const qc = useQueryClient()
  const adapters = useAdapters()
  const available = useAvailableAdapters()
  const pending = usePendingRestart()

  const [tab, setTab] = useState<Tab>('providers')
  const [editing, setEditing] = useState<EditingState | null>(null)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
    void qc.invalidateQueries({ queryKey: ['config', 'pending-restart'] })
  }

  async function onToggle(inst: AdapterInstance) {
    await updateAdapter(inst.id, {
      name: inst.name,
      enabled: !inst.enabled,
      priority: inst.priority,
      config: stripIsSet(inst.config),
    })
    refresh()
  }

  async function onReorder(inst: AdapterInstance, delta: number) {
    await updateAdapter(inst.id, {
      name: inst.name,
      enabled: inst.enabled,
      priority: inst.priority + delta,
      config: stripIsSet(inst.config),
    })
    refresh()
  }

  async function onRemove(id: string) {
    await deleteAdapter(id)
    refresh()
  }

  function onTest(inst: AdapterInstance) {
    // Open edit form — AdapterForm has built-in Test Connection
    const avail = available.data ?? []
    const schema = avail.find((a) => a.name === inst.name)?.configSchema ?? { fields: [] }
    setEditing({ section: inst.type, instance: inst })
    void schema
  }

  function onEdit(inst: AdapterInstance) {
    setEditing({ section: inst.type, instance: inst })
  }

  const list = adapters.data ?? []
  const avail = available.data ?? []
  const isLoading = adapters.isLoading || available.isLoading
  const pendingRestart = pending.data?.pendingRestart ?? false

  const libraryInstances = list.filter((a) => a.type === 'library')
  const searchInstances = list.filter((a) => a.type === 'search')
  const downloaderInstances = list.filter((a) => a.type === 'downloader')

  const libraryAvail = avail.filter((a) => a.type === 'library')
  const searchAvail = avail.filter((a) => a.type === 'search')
  const downloaderAvail = avail.filter((a) => a.type === 'downloader')

  // Determine the schema + name for the edit/add form
  const editingSchema =
    editing?.add?.configSchema ??
    avail.find((a) => a.name === editing?.instance?.name)?.configSchema ??
    { fields: [] }
  const editingName = editing?.add?.name ?? editing?.instance?.name ?? ''

  return (
    <div className="max-w-4xl space-y-6 pb-8">
      {/* Header */}
      <h1 className="text-3xl font-black tracking-tight text-text-primary">Admin</h1>

      {/* Tabs */}
      <div className="flex gap-2 border-b border-border-subtle pb-0" role="tablist" aria-label="Admin sections">
        <Chip selected={tab === 'providers'} onClick={() => setTab('providers')}>
          Providers
        </Chip>
        <Chip selected={tab === 'server'} onClick={() => setTab('server')}>
          Server
        </Chip>
        <Chip selected={tab === 'users'} onClick={() => setTab('users')}>
          Users
        </Chip>
      </div>

      {/* ── Providers tab ── */}
      {tab === 'providers' && (
        <div className="space-y-8">
          <RestartBanner show={pendingRestart} />

          {isLoading ? (
            <div className="space-y-4" aria-label="Loading providers">
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : (
            <>
              <AdapterSection
                title="Library providers"
                subtitle="Scans your local music files"
                type="library"
                instances={libraryInstances}
                available={libraryAvail}
                onAdd={() => {
                  const choice = libraryAvail[0]
                  if (choice) setEditing({ section: 'library', add: choice })
                }}
                onTest={onTest}
                onEdit={onEdit}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
                pendingRestart={pendingRestart}
              />

              <AdapterSection
                title="Search providers"
                subtitle="Priority-ordered — first match wins"
                type="search"
                instances={searchInstances}
                available={searchAvail}
                onAdd={() => {
                  const choice = searchAvail[0]
                  if (choice) setEditing({ section: 'search', add: choice })
                }}
                onTest={onTest}
                onEdit={onEdit}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
                onReorder={(inst, delta) => void onReorder(inst, delta)}
                pendingRestart={pendingRestart}
              />

              <AdapterSection
                title="Downloaders"
                subtitle="Fallback chain — tried in order"
                type="downloader"
                instances={downloaderInstances}
                available={downloaderAvail}
                onAdd={() => {
                  const choice = downloaderAvail[0]
                  if (choice) setEditing({ section: 'downloader', add: choice })
                }}
                onTest={onTest}
                onEdit={onEdit}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
                onReorder={(inst, delta) => void onReorder(inst, delta)}
                pendingRestart={pendingRestart}
              />
            </>
          )}

          {/* Add / Edit form panel */}
          {editing && (
            <div
              role="dialog"
              aria-modal="true"
              aria-label={editing.add ? `Add ${editingName}` : `Edit ${editingName}`}
              className="rounded-lg border border-border-subtle bg-raised p-6 space-y-4"
            >
              <h3 className="text-base font-extrabold text-text-primary">
                {editing.add ? `Add ${editingName}` : `Edit ${editingName}`}
              </h3>
              <AdapterForm
                name={editingName}
                schema={editingSchema}
                initial={editing.instance?.config}
                submitLabel={editing.add ? 'Add' : 'Save'}
                onSubmit={async (config) => {
                  if (editing.add) {
                    await createAdapter({
                      type: editing.section,
                      name: editingName,
                      enabled: true,
                      priority: 0,
                      config,
                    })
                  } else if (editing.instance) {
                    await updateAdapter(editing.instance.id, {
                      name: editing.instance.name,
                      enabled: editing.instance.enabled,
                      priority: editing.instance.priority,
                      config,
                    })
                  }
                  setEditing(null)
                  refresh()
                }}
              />
              <button
                type="button"
                onClick={() => setEditing(null)}
                className="text-sm text-text-muted hover:text-text-secondary transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                Cancel
              </button>
            </div>
          )}
        </div>
      )}

      {/* ── Server tab ── */}
      {tab === 'server' && (
        <div className="space-y-6">
          <div className="rounded-lg border border-border-subtle bg-raised p-6 space-y-3">
            <h2 className="text-base font-extrabold text-text-primary">Server info</h2>
            <p className="text-sm text-text-secondary">
              Reverb is running. Configuration changes take effect after a restart.
            </p>
            <div className="space-y-2 pt-1">
              <div className="text-sm font-bold text-text-primary">Applying config changes</div>
              <p className="text-xs text-text-secondary">
                After adding, editing, or removing providers, restart the Reverb server process to
                pick up the new configuration. The banner at the top of the Providers tab will
                remind you when a restart is needed.
              </p>
            </div>
          </div>
        </div>
      )}

      {/* ── Users tab ── */}
      {tab === 'users' && (
        <EmptyState
          icon="browse"
          title="Single-admin for now"
          hint="Multi-user support is on the roadmap — for now Reverb has one admin account."
        />
      )}
    </div>
  )
}
