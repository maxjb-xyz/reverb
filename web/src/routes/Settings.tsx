import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Chip, Toggle } from '../components/ui'
import { AccentSwatches } from '../components/AccentSwatches'
import { useSettings, useUpdateSettings } from '../lib/settingsApi'
import { useAdapters, updateAdapter, type AdapterInstance } from '../lib/adaptersApi'

type Tab = 'appearance'

export default function Settings() {
  const [tab, setTab] = useState<Tab>('appearance')
  const qc = useQueryClient()
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const adapters = useAdapters()

  // Enabled downloaders that have granularities data
  const enabledDownloaders = (adapters.data ?? []).filter(
    (a) => a.type === 'downloader' && a.enabled && a.granularities != null,
  )

  // Song column: instances with a "track" granularity, sorted ascending by track order
  const songDownloaders = enabledDownloaders
    .filter((a) => 'track' in (a.granularities ?? {}))
    .slice()
    .sort((a, b) => (a.granularities!['track'] ?? 0) - (b.granularities!['track'] ?? 0))

  // Album column: instances with an "album" granularity, sorted ascending by album order
  const albumDownloaders = enabledDownloaders
    .filter((a) => 'album' in (a.granularities ?? {}))
    .slice()
    .sort((a, b) => (a.granularities!['album'] ?? 0) - (b.granularities!['album'] ?? 0))

  const anyDownloaders = songDownloaders.length > 0 || albumDownloaders.length > 0

  /**
   * Swap the order value for granularity `g` between two adjacent instances in a column.
   * Only the `g`-key in each instance's granularities map is mutated; all other keys
   * (e.g. the other granularity's order) are untouched.
   */
  async function moveInColumn(
    column: AdapterInstance[],
    index: number,
    direction: 'up' | 'down',
    g: string,
  ) {
    const swapIndex = direction === 'up' ? index - 1 : index + 1
    const a = column[index]
    const b = column[swapIndex]
    const aNewGranularities = { ...a.granularities, [g]: b.granularities![g] }
    const bNewGranularities = { ...b.granularities, [g]: a.granularities![g] }
    await Promise.all([
      updateAdapter(a.id, {
        name: a.name,
        enabled: a.enabled,
        priority: a.priority,
        config: { ...a.config, granularities: aNewGranularities },
      }),
      updateAdapter(b.id, {
        name: b.name,
        enabled: b.enabled,
        priority: b.priority,
        config: { ...b.config, granularities: bNewGranularities },
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

      {/* Downloaders section — two-column layout: Song | Album, each independently ordered */}
      {anyDownloaders && (
        <section className="space-y-3">
          <div>
            <h2 className="text-lg font-extrabold tracking-tight text-text-primary">Downloaders</h2>
            <p className="text-xs text-text-secondary mt-0.5">
              Fallback chain per granularity — tried in order. Reorder each column independently.
            </p>
          </div>
          <div className="grid grid-cols-2 gap-4">
            {/* Song column */}
            <div data-testid="downloaders-song-col">
              <div className="text-xs font-bold uppercase tracking-widest text-text-secondary mb-2">
                Song
              </div>
              <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-raised">
                {songDownloaders.map((dl, i) => (
                  <div key={dl.id} className="flex items-center gap-4 px-4 py-3">
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
                        onClick={() => void moveInColumn(songDownloaders, i, 'up', 'track')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8593;
                      </button>
                      <button
                        aria-label="Move down"
                        disabled={i === songDownloaders.length - 1}
                        onClick={() => void moveInColumn(songDownloaders, i, 'down', 'track')}
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
              <div className="divide-y divide-border-subtle rounded-lg border border-border-subtle bg-raised">
                {albumDownloaders.map((dl, i) => (
                  <div key={dl.id} className="flex items-center gap-4 px-4 py-3">
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
                        onClick={() => void moveInColumn(albumDownloaders, i, 'up', 'album')}
                        className="rounded p-1 text-text-secondary hover:text-text-primary disabled:opacity-30 disabled:cursor-not-allowed"
                      >
                        &#8593;
                      </button>
                      <button
                        aria-label="Move down"
                        disabled={i === albumDownloaders.length - 1}
                        onClick={() => void moveInColumn(albumDownloaders, i, 'down', 'album')}
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
        </section>
      )}

    </div>
  )
}
