import type { ReactNode } from 'react'
import { Segmented, Select } from '../ui'
import {
  KEEP_RULES,
  useDiscoverPrefs,
  useDiscoverSettings,
  useSaveDiscoverPrefs,
  useSaveDiscoverSettings,
  type DiscoverPrefs,
  type KeepRule,
} from '../../lib/discoverApi'
import { useToastStore } from '../../lib/toastStore'

const SIZES = [10, 20, 30, 50, 100]
const SHARES = [
  { value: '100', label: 'All new to me' },
  { value: '70', label: 'Mostly new to me' },
  { value: '50', label: 'Half new, half my library' },
  { value: '30', label: 'Mostly my library' },
]
const CAPS = [0, 10, 20, 30, 50, 100]

function Row({ title, hint, children }: { title: string; hint: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:gap-5">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-bold text-text-primary">{title}</div>
        <div className="mt-0.5 text-xs text-text-secondary">{hint}</div>
      </div>
      <div className="flex-none">{children}</div>
    </div>
  )
}

/** Discover Weekly preferences, plus the server-wide download switch for managers. */
export function DiscoverSettings({ isManager }: { isManager: boolean }) {
  const { data: prefs, isError } = useDiscoverPrefs()
  const save = useSaveDiscoverPrefs()
  const push = useToastStore((s) => s.push)
  const set = (patch: Partial<DiscoverPrefs>) => {
    if (!prefs) return
    save.mutate({ ...prefs, ...patch }, { onError: () => push("Couldn't save Discover Weekly settings", 'error') })
  }
  const current = save.isPending && save.variables ? save.variables : prefs
  // A share set through the API may not be one of the steps; show the nearest.
  const share = current
    ? SHARES.reduce((best, o) => (Math.abs(Number(o.value) - current.newShare) < Math.abs(Number(best.value) - current.newShare) ? o : best)).value
    : '70'
  const keep = KEEP_RULES.find((k) => k.value === current?.keepRule)

  return (
    <section aria-label="Discover Weekly" className="border-b border-border-subtle pb-2">
      <div className="text-sm font-bold text-text-primary">Discover Weekly</div>
      <div className="mt-0.5 text-xs text-text-secondary">
        A new list every Monday, picked from artists like the ones you love. Songs that aren&apos;t in your library are downloaded ahead of time when
        you&apos;re allowed to add music.
      </div>
      {isError && <div className="mt-2 text-sm text-error">Couldn&apos;t load Discover Weekly settings.</div>}
      {current && (
        <div className="divide-y divide-border-subtle">
          <Row title="Discover Weekly" hint="Build a list for you each week.">
            <Segmented
              options={[
                { value: 'on', label: 'On' },
                { value: 'off', label: 'Off' },
              ]}
              value={current.enabled ? 'on' : 'off'}
              onChange={(v) => set({ enabled: v === 'on' })}
            />
          </Row>
          {current.enabled && (
            <>
              <Row title="Length" hint="How many songs each week.">
                <Select label="Discover length" value={String(current.size)} onChange={(v) => set({ size: Number(v) })} options={SIZES.map((n) => ({ value: String(n), label: `${n} songs` }))} />
              </Row>
              <Row title="Mix" hint="How much of it is music you don't have yet.">
                <Select label="Discover mix" value={share} onChange={(v) => set({ newShare: Number(v) })} options={SHARES} />
              </Row>
              <Row title="Keep downloaded songs" hint={keep?.hint ?? ''}>
                <Select label="Keep downloaded songs" value={current.keepRule} onChange={(v) => set({ keepRule: v as KeepRule })} options={KEEP_RULES} />
              </Row>
            </>
          )}
          {isManager && <ServerRows />}
        </div>
      )}
    </section>
  )
}

function ServerRows() {
  const { data } = useDiscoverSettings(true)
  const save = useSaveDiscoverSettings()
  const push = useToastStore((s) => s.push)
  if (!data) return null
  const current = save.isPending && save.variables ? save.variables : data
  const set = (patch: Partial<typeof data>) =>
    save.mutate({ ...current, ...patch }, { onError: () => push("Couldn't save the Discover download settings", 'error') })
  const caps = CAPS.includes(current.weeklyCap) ? CAPS : [...CAPS, current.weeklyCap].sort((a, b) => a - b)
  return (
    <>
      <Row title="Download Discover songs ahead (everyone)" hint="Only for people who can add music. Songs are fetched when the week's list is built.">
        <Segmented
          options={[
            { value: 'on', label: 'On' },
            { value: 'off', label: 'Off' },
          ]}
          value={current.downloads ? 'on' : 'off'}
          onChange={(v) => set({ downloads: v === 'on' })}
        />
      </Row>
      {current.downloads && (
        <Row title="Weekly download limit" hint="Most songs Discover downloads for one person in a week.">
          <Select
            label="Weekly download limit"
            value={String(current.weeklyCap)}
            onChange={(v) => set({ weeklyCap: Number(v) })}
            options={caps.map((n) => ({ value: String(n), label: n === 0 ? 'None' : `${n} songs` }))}
          />
        </Row>
      )}
    </>
  )
}
