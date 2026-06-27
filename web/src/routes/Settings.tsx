import { useState } from 'react'
import { Chip, Toggle, Select } from '../components/ui'
import { AccentSwatches } from '../components/AccentSwatches'
import { useSettings, useUpdateSettings } from '../lib/settingsApi'
import { useAdapters } from '../lib/adaptersApi'

type Tab = 'appearance'

export default function Settings() {
  const [tab, setTab] = useState<Tab>('appearance')
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const adapters = useAdapters()
  const downloaders = (adapters.data ?? [])
    .filter((a) => a.type === 'downloader' && a.enabled)
    .sort((a, b) => a.priority - b.priority)
  const downloaderOptions = [
    { value: '', label: 'Always ask' },
    ...downloaders.map((d) => ({ value: d.name, label: d.name })),
  ]

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

          {/* Default downloader row */}
          <div className="flex items-center gap-5 py-5">
            <div className="flex-1 min-w-0">
              <div className="text-sm font-bold text-text-primary">Default downloader</div>
              <div className="text-xs text-text-secondary mt-0.5">
                Skip the picker and use this downloader for one-click downloads. &ldquo;Always ask&rdquo; shows the picker when more than one is enabled.
              </div>
            </div>
            <div className="flex-none">
              <Select
                label="Default downloader"
                value={settings.data?.defaultDownloader ?? ''}
                options={downloaderOptions}
                onChange={(v) => updateSettings.mutate({ defaultDownloader: v })}
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

    </div>
  )
}
