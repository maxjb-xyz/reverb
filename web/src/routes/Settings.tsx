import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Chip, Toggle } from '../components/ui'
import { AccentSwatches } from '../components/AccentSwatches'
import { useSettings, useUpdateSettings } from '../lib/settingsApi'
import { useAdapters, updateAdapter, type AdapterInstance } from '../lib/adaptersApi'

type Tab = 'appearance'

function granularityLabel(a: AdapterInstance): string {
  return a.capabilities.includes('grain:album') ? 'Album' : 'Track'
}

export default function Settings() {
  const [tab, setTab] = useState<Tab>('appearance')
  const qc = useQueryClient()
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const adapters = useAdapters()

  const downloaders = (adapters.data ?? [])
    .filter((a) => a.type === 'downloader' && a.enabled)
    .slice()
    .sort((a, b) => a.priority - b.priority)

  async function moveDownloader(index: number, direction: 'up' | 'down') {
    const swapIndex = direction === 'up' ? index - 1 : index + 1
    const a = downloaders[index]
    const b = downloaders[swapIndex]
    // These two updateAdapter calls are non-atomic; a partial failure leaves a
    // duplicate priority — acceptable for an admin-only, low-concurrency reorder
    // (DB ordering stays stable on reload).
    await Promise.all([
      updateAdapter(a.id, {
        name: a.name,
        enabled: a.enabled,
        priority: b.priority,
        config: a.config,
      }),
      updateAdapter(b.id, {
        name: b.name,
        enabled: b.enabled,
        priority: a.priority,
        config: b.config,
      }),
    ])
    void qc.invalidateQueries({ queryKey: ['adapters', 'list'] })
  }

  return (
    <div className="max-w-4xl space-y-6 pb-8">
      {/* Header */}
      <h1 className="text-3xl font-black tracking-tight text-text-primary">Settings</h1>

      {/* Tabs */}
      <div className="flex gap-2 border-b border-border-subtle pb-0">
        <Chip selected={tab === 'appearance'} onClick={() => setTab('appearance')}>
          Appearance
        </Chip>
      </div>

      {/* Appearance tab */}
      {tab === 'appearance' && (
        <div className="space-y-0 divide-y divide-border-subtle">
          {/* Accent color row */}
          <div className="flex items-start gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Accent color</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Tints buttons, highlights, progress and the player. Default is red — pick a preset or a custom hex.
              </div>
            </div>
            <div className="flex-none">
              <AccentSwatches />
            </div>
          </div>

          {/* Dynamic album background row */}
          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Dynamic album background</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Wash the background with the dominant color of the album that&apos;s playing.
              </div>
            </div>
            <div className="flex-none">
              <Toggle
                checked={settings.data?.dynamicBackground ?? true}
                label="Dynamic album background"
                onChange={(v) => {
                  updateSettings.mutate({ dynamicBackground: v })
                }}
              />
            </div>
          </div>

          {/* Theme row — dark only, honest */}
          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Theme</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Dark-first. Light theme is on the roadmap.
              </div>
            </div>
            <div className="flex-none">
              <span className="text-sm font-semibold text-text-secondary">Dark</span>
            </div>
          </div>
        </div>
      )}

      {/* Downloaders section — always visible, shows enabled downloaders sorted by priority */}
      {downloaders.length > 0 && (
        <section className="space-y-3">
          <div>
            <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Downloaders</h2>
            <p className="text-xs text-text-secondary mt-0.5">
              Fallback chain — tried in order. Reorder with the arrows.
            </p>
          </div>
          <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-raised">
            {downloaders.map((dl, i) => (
              <div key={dl.id} className="flex items-center gap-4 px-4 py-3">
                <div className="flex-1 min-w-0">
                  <span className="text-sm font-semibold text-text-primary">{dl.name}</span>
                  <span className="ml-2 text-xs text-text-secondary font-medium">
                    {granularityLabel(dl)}
                  </span>
                </div>
                <div className="flex gap-1">
                  <button
                    aria-label="Move up"
                    disabled={i === 0}
                    onClick={() => void moveDownloader(i, 'up')}
                    className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                  >
                    &#8593;
                  </button>
                  <button
                    aria-label="Move down"
                    disabled={i === downloaders.length - 1}
                    onClick={() => void moveDownloader(i, 'down')}
                    className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                  >
                    &#8595;
                  </button>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

    </div>
  )
}
