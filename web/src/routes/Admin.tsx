import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  useAdapters,
  useAvailableAdapters,
  createAdapter,
  updateAdapter,
  deleteAdapter,
  type AdapterInstance,
} from '../lib/adaptersApi'
import { AdapterSection } from '../components/admin/AdapterSection'
import { AdapterForm } from '../components/AdapterForm'
import { Chip, Skeleton, EmptyState, Select, Button } from '../components/ui'
import { useSettings, useUpdateSettings } from '../lib/settingsApi'
import { useLibraryStatus } from '../lib/libraryApi'

const modeLabel = (m: string) => (m === 'external' ? 'External Subsonic' : 'Built-in (bundled)')

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

export default function Admin() {
  const qc = useQueryClient()
  const adapters = useAdapters()
  const available = useAvailableAdapters()
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const libStatus = useLibraryStatus()

  const [tab, setTab] = useState<Tab>('providers')
  // The mode the user has picked in the dropdown but not yet applied. null = no
  // pending pick (the dropdown reflects the saved mode).
  const [pickedMode, setPickedMode] = useState<'built-in' | 'external' | null>(null)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
  }

  // CRUD — every mutation applies live (the server hot-reloads the active services).
  async function onCreate(type: string, name: string, config: Record<string, unknown>) {
    await createAdapter({ type, name, enabled: true, priority: 0, config })
    refresh()
  }

  async function onUpdate(inst: AdapterInstance, config: Record<string, unknown>) {
    await updateAdapter(inst.id, {
      name: inst.name,
      enabled: inst.enabled,
      priority: inst.priority,
      config,
    })
    refresh()
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

  const list = adapters.data ?? []
  const avail = available.data ?? []
  const isLoading = adapters.isLoading || available.isLoading

  const searchInstances = list.filter((a) => a.type === 'search')
  const downloaderInstances = list.filter((a) => a.type === 'downloader')

  const searchAvail = avail.filter((a) => a.type === 'search')
  const downloaderAvail = avail.filter((a) => a.type === 'downloader')

  // Library is single-active: a "switch", not a list — Built-in (bundled, no
  // config) or External (one Subsonic server).
  const libProvider = avail.find((a) => a.type === 'library') ?? null
  const libInstance = list.find((a) => a.type === 'library') ?? null
  const hasEnabledLibrary = list.some((a) => a.type === 'library' && a.enabled)

  // savedMode = the persisted desired mode. The stored setting is "" until picked
  // explicitly, so mirror the backend's ResolveMode: unset → External when a
  // library adapter is configured (existing deployments stay external), else
  // Built-in. selectedMode = what the dropdown shows (a pending pick, or saved).
  const rawMode = settings.data?.libraryBackendMode
  const savedMode: 'built-in' | 'external' =
    rawMode === 'external'
      ? 'external'
      : rawMode === 'built-in'
        ? 'built-in'
        : hasEnabledLibrary
          ? 'external'
          : 'built-in'
  const selectedMode = pickedMode ?? savedMode
  const pendingApply = selectedMode !== savedMode
  // runningMode = the mode the server booted with (changes need a restart). A
  // saved change that hasn't been restarted shows runningMode !== savedMode.
  const runningMode = libStatus.data?.mode
  const pendingRestart = !!runningMode && runningMode !== savedMode

  function applyMode() {
    updateSettings.mutate(
      { libraryBackendMode: selectedMode },
      { onSuccess: () => setPickedMode(null) },
    )
  }

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
          {/* Library backend — single-active switch (Built-in | External Subsonic) */}
          <section className="rounded-lg border border-border-subtle bg-raised p-6 space-y-4">
            <div className="flex items-center justify-between gap-5">
              <div className="flex-1 min-w-0">
                <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Library backend</h2>
                <p className="text-xs text-text-secondary mt-0.5">
                  Where your music collection lives. Only one is active at a time.
                </p>
              </div>
              <div className="flex-none">
                <Select
                  label="Library backend"
                  value={selectedMode}
                  options={[
                    { value: 'built-in', label: 'Built-in (bundled)' },
                    { value: 'external', label: 'External Subsonic' },
                  ]}
                  onChange={(v) => setPickedMode(v as 'built-in' | 'external')}
                />
              </div>
            </div>

            {/* A saved change that hasn't been restarted yet. */}
            {pendingRestart && (
              <div
                role="status"
                className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-200"
              >
                <span className="font-semibold">Running {modeLabel(runningMode!)}.</span>{' '}
                Restart Reverb to apply {modeLabel(savedMode)}.
              </div>
            )}

            {selectedMode === 'built-in' ? (
              <p className="text-xs text-text-muted border-t border-border-subtle pt-4">
                Reverb runs a bundled music server that scans the folder mounted at{' '}
                <code>/music</code> (set <code>REVERB_DOWNLOAD_DIR</code> to change it). No external
                setup needed.
              </p>
            ) : (
              <div className="space-y-3 border-t border-border-subtle pt-4">
                <h3 className="text-sm font-extrabold text-text-primary">
                  {libInstance ? 'Subsonic server' : 'Connect your Subsonic server'}
                </h3>
                {available.isLoading ? (
                  <Skeleton className="h-20 w-full" />
                ) : libProvider ? (
                  <AdapterForm
                    name={libProvider.name}
                    schema={libProvider.configSchema}
                    initial={libInstance?.config}
                    submitLabel="Save"
                    onSubmit={async (config) => {
                      if (libInstance) await onUpdate(libInstance, config)
                      else await onCreate('library', libProvider.name, config)
                    }}
                  />
                ) : (
                  <p className="text-sm text-text-muted">No library provider is registered.</p>
                )}
                {!libInstance && (
                  <p className="text-xs text-amber-300/80">
                    Save your Subsonic server above before applying — otherwise the library will be
                    unavailable after the restart.
                  </p>
                )}
              </div>
            )}

            {/* Mode changes are restart-only, so apply is explicit (not save-on-change). */}
            {pendingApply && (
              <div className="flex items-center gap-3 border-t border-border-subtle pt-4">
                <Button size="sm" variant="primary" onClick={applyMode}>
                  Apply (requires restart)
                </Button>
                <span className="text-xs text-text-muted">
                  Switches to <span className="font-semibold">{modeLabel(selectedMode)}</span> the
                  next time Reverb restarts.
                </span>
              </div>
            )}
          </section>

          {isLoading ? (
            <div className="space-y-4" aria-label="Loading providers">
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-6 w-40" />
              <Skeleton className="h-20 w-full" />
            </div>
          ) : (
            <>
              <AdapterSection
                title="Search providers"
                subtitle="Priority-ordered — first match wins"
                type="search"
                instances={searchInstances}
                available={searchAvail}
                onCreate={(name, config) => onCreate('search', name, config)}
                onUpdate={onUpdate}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
                onReorder={(inst, delta) => void onReorder(inst, delta)}
              />

              <AdapterSection
                title="Downloaders"
                subtitle="Fallback chain — tried in order"
                type="downloader"
                instances={downloaderInstances}
                available={downloaderAvail}
                onCreate={(name, config) => onCreate('downloader', name, config)}
                onUpdate={onUpdate}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
                onReorder={(inst, delta) => void onReorder(inst, delta)}
              />
            </>
          )}
        </div>
      )}

      {/* ── Server tab ── */}
      {tab === 'server' && (
        <div className="space-y-6">
          <div className="rounded-lg border border-border-subtle bg-raised p-6 space-y-3">
            <h2 className="text-base font-extrabold text-text-primary">Server info</h2>
            <p className="text-sm text-text-secondary">
              Reverb is running. Configuration changes take effect immediately.
            </p>
            <div className="space-y-2 pt-1">
              <div className="text-sm font-bold text-text-primary">Applying config changes</div>
              <p className="text-xs text-text-secondary">
                Adding, editing, or removing providers applies live — Reverb rebuilds the active
                library, search, and downloader the moment you save. No restart required.
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
