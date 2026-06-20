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
import { useSettings, putSettings, applyAccent } from '../lib/settingsApi'
import { AdapterForm } from '../components/AdapterForm'

const SECTIONS: { type: string; title: string }[] = [
  { type: 'library', title: 'Library' },
  { type: 'search', title: 'Search' },
  { type: 'downloader', title: 'Downloaders' },
]

// stripIsSet removes the "<key>__isSet" sidecars before re-sending a redacted config.
function stripIsSet(config: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const k of Object.keys(config)) {
    if (k.endsWith('__isSet')) continue
    out[k] = config[k]
  }
  return out
}

export default function Settings() {
  const qc = useQueryClient()
  const adapters = useAdapters()
  const available = useAvailableAdapters()
  const pending = usePendingRestart()
  const settings = useSettings()

  const [editing, setEditing] = useState<{ section: string; instance?: AdapterInstance; add?: AvailableAdapter } | null>(null)

  function refresh() {
    void qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
    void qc.invalidateQueries({ queryKey: ['config', 'pending-restart'] })
  }

  async function onRemove(id: string) {
    await deleteAdapter(id)
    refresh()
  }
  async function onToggle(inst: AdapterInstance) {
    await updateAdapter(inst.id, { name: inst.name, enabled: !inst.enabled, priority: inst.priority, config: stripIsSet(inst.config) })
    refresh()
  }
  async function onReorder(inst: AdapterInstance, delta: number) {
    await updateAdapter(inst.id, { name: inst.name, enabled: inst.enabled, priority: inst.priority + delta, config: stripIsSet(inst.config) })
    refresh()
  }

  const list = adapters.data ?? []
  const avail = available.data ?? []

  return (
    <div className="max-w-2xl space-y-8">
      <h1 className="text-2xl font-bold">Settings</h1>

      {pending.data?.pendingRestart && (
        <div className="rounded border border-accent/50 bg-accent/10 px-4 py-3 text-sm text-accent">
          Restart Crate to apply your configuration changes.
        </div>
      )}

      {SECTIONS.map((sec) => {
        const items = list.filter((a) => a.type === sec.type).sort((a, b) => a.priority - b.priority)
        const choices = avail.filter((a) => a.type === sec.type)
        return (
          <section key={sec.type} className="space-y-2">
            <h2 className="text-lg font-semibold">{sec.title}</h2>
            <ul className="space-y-1">
              {items.length === 0 && <li className="text-sm text-neutral-500">None configured.</li>}
              {items.map((inst) => (
                <li key={inst.id} className="flex items-center gap-2 rounded bg-neutral-900 px-3 py-2">
                  <span className="flex-1">{inst.name}</span>
                  <button type="button" aria-label={`Toggle ${inst.id}`} onClick={() => void onToggle(inst)} className="text-sm text-neutral-300">
                    {inst.enabled ? 'Enabled' : 'Disabled'}
                  </button>
                  <button type="button" aria-label={`Move up ${inst.id}`} onClick={() => void onReorder(inst, -1)} className="text-neutral-400">↑</button>
                  <button type="button" aria-label={`Move down ${inst.id}`} onClick={() => void onReorder(inst, 1)} className="text-neutral-400">↓</button>
                  <button type="button" aria-label={`Edit ${inst.id}`} onClick={() => setEditing({ section: sec.type, instance: inst })} className="text-sm text-neutral-300">Edit</button>
                  <button type="button" aria-label={`Remove ${inst.id}`} onClick={() => void onRemove(inst.id)} className="text-sm text-accent">Remove</button>
                </li>
              ))}
            </ul>
            <div className="flex flex-wrap gap-2">
              {choices.map((c) => (
                <button key={c.name} type="button" onClick={() => setEditing({ section: sec.type, add: c })} className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800">
                  + Add {c.name}
                </button>
              ))}
            </div>
          </section>
        )
      })}

      {editing && (() => {
        const schema = editing.add?.configSchema ?? avail.find((a) => a.name === editing.instance?.name)?.configSchema ?? { fields: [] }
        const name = editing.add?.name ?? editing.instance?.name ?? ''
        const initial = editing.instance?.config
        return (
          <div className="rounded border border-neutral-700 p-4">
            <h3 className="mb-3 font-semibold">{editing.add ? `Add ${name}` : `Edit ${name}`}</h3>
            <AdapterForm
              name={name}
              schema={schema}
              initial={initial}
              submitLabel={editing.add ? 'Add' : 'Save'}
              onSubmit={async (config) => {
                if (editing.add) {
                  await createAdapter({ type: editing.section, name, enabled: true, priority: 0, config })
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
            <button type="button" onClick={() => setEditing(null)} className="mt-2 text-sm text-neutral-400">Cancel</button>
          </div>
        )
      })()}

      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Appearance</h2>
        <div className="flex items-center gap-3">
          <label htmlFor="accent" className="text-sm text-neutral-300">Accent color</label>
          <input
            id="accent"
            type="color"
            value={settings.data?.accentColor ?? '#F0354B'}
            onChange={(e) => {
              applyAccent(e.target.value)
              void putSettings({ accentColor: e.target.value }).then(() => qc.invalidateQueries({ queryKey: ['settings'] }))
            }}
          />
        </div>
        <div className="flex items-center gap-2">
          <input
            id="dynbg"
            type="checkbox"
            checked={settings.data?.dynamicBackground ?? true}
            onChange={(e) => void putSettings({ dynamicBackground: e.target.checked }).then(() => qc.invalidateQueries({ queryKey: ['settings'] }))}
          />
          <label htmlFor="dynbg" className="text-sm text-neutral-300">Dynamic album background</label>
        </div>
      </section>
    </div>
  )
}
