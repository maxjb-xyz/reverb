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

export default function Admin() {
  const qc = useQueryClient()
  const adapters = useAdapters()
  const available = useAvailableAdapters()

  const [tab, setTab] = useState<Tab>('providers')

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

  const libraryInstances = list.filter((a) => a.type === 'library')
  const searchInstances = list.filter((a) => a.type === 'search')
  const downloaderInstances = list.filter((a) => a.type === 'downloader')

  const libraryAvail = avail.filter((a) => a.type === 'library')
  const searchAvail = avail.filter((a) => a.type === 'search')
  const downloaderAvail = avail.filter((a) => a.type === 'downloader')

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
                subtitle="Where your music collection lives"
                type="library"
                instances={libraryInstances}
                available={libraryAvail}
                onCreate={(name, config) => onCreate('library', name, config)}
                onUpdate={onUpdate}
                onToggle={(inst) => void onToggle(inst)}
                onRemove={(id) => void onRemove(id)}
              />

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
