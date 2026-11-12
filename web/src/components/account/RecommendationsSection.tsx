import { Button, EmptyState } from '../ui'
import { useTasteFeedback, useUnhide } from '../../lib/tasteApi'
import { useAnalysisStatus, useIndexStatus, useRebuildIndex, useRunAnalysis } from '../../lib/engineApi'
import { useAuthStore, isManagerCaps } from '../../lib/authStore'
import { DiscoverSettings } from './DiscoverSettings'

/** Managers see how much of the library the recommendation index covers. */
function IndexStatusRow() {
  const { data, isError } = useIndexStatus(true)
  const rebuild = useRebuildIndex()
  const nf = new Intl.NumberFormat()
  let text = 'Checking…'
  if (isError) text = "Couldn't load the index status."
  else if (data?.running) text = `Indexing… ${nf.format(data.tracks)} songs so far.`
  else if (data && data.tracks === 0) text = 'Not indexed yet. It runs shortly after Reverb starts and whenever the library changes.'
  else if (data)
    text = `${nf.format(data.tracks)} songs by ${nf.format(data.artists)} artists. Similar artists found for ${nf.format(data.artistsWithSimilar)} of them.`
  return (
    <div className="flex items-start gap-5 border-b border-border-subtle pb-5">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-bold text-text-primary">Recommendation index</div>
        <div className="mt-0.5 text-xs text-text-secondary">{text}</div>
        {data?.lastError && <div className="mt-1 text-xs text-error">Last run failed: {data.lastError}</div>}
      </div>
      <Button variant="secondary" size="sm" disabled={rebuild.isPending || data?.running} onClick={() => rebuild.mutate()}>
        Rebuild now
      </Button>
    </div>
  )
}

/** Managers see how far the optional audio analysis has got. Hidden until an analyzer is set up. */
function AnalysisStatusRow() {
  const { data } = useAnalysisStatus(true)
  const run = useRunAnalysis()
  if (!data?.enabled) return null
  const nf = new Intl.NumberFormat()
  let text = `${nf.format(data.analyzed)} of ${nf.format(data.total)} songs analyzed.`
  if (data.running) text = `Analyzing… ${text}`
  if (data.failed) text += ` ${nf.format(data.failed)} couldn't be read and will be tried again later.`
  if (data.analyzed < 50) text += ' Radio starts adding songs that sound alike once 50 are done.'
  return (
    <div className="flex items-start gap-5 border-b border-border-subtle pb-5" data-testid="analysis-status">
      <div className="min-w-0 flex-1">
        <div className="text-sm font-bold text-text-primary">Audio analysis</div>
        <div className="mt-0.5 text-xs text-text-secondary">{text}</div>
        {data.lastError && <div className="mt-1 text-xs text-error">Last problem: {data.lastError}</div>}
      </div>
      <Button variant="secondary" size="sm" disabled={run.isPending || data.running || data.analyzed + data.failed >= data.total} onClick={() => run.mutate()}>
        Continue now
      </Button>
    </div>
  )
}

/**
 * Settings → Recommendations: Discover Weekly, then the songs and artists the
 * user said "not for me" to, with a way to bring each back.
 */
export function RecommendationsSection() {
  const { data, isLoading, isError } = useTasteFeedback()
  const unhide = useUnhide()
  const isManager = useAuthStore((st) => isManagerCaps(st.me?.capabilities))
  const items = data ?? []
  const artists = items.filter((f) => f.targetKind === 'artist')
  const tracks = items.filter((f) => f.targetKind === 'track')

  return (
    <div className="space-y-6">
      {isManager && <IndexStatusRow />}
      {isManager && <AnalysisStatusRow />}
      <DiscoverSettings isManager={isManager} />
      <div>
        <div className="text-sm font-bold text-text-primary">Hidden from recommendations</div>
        <div className="mt-0.5 text-xs text-text-secondary">
          Songs and artists you marked &ldquo;Not for me&rdquo; stay out of radio and mixes. You can still play them yourself.
        </div>
      </div>

      {isLoading && <div className="text-sm text-text-secondary">Loading…</div>}
      {isError && <div className="text-sm text-error">Couldn&apos;t load this list. Try again.</div>}
      {!isLoading && !isError && items.length === 0 && (
        <EmptyState icon="thumbs-down" title="Nothing hidden" hint="Use the thumbs-down in the player to tell Reverb a song or artist isn't for you." />
      )}

      {[
        { label: 'Artists', rows: artists },
        { label: 'Songs', rows: tracks },
      ]
        .filter((g) => g.rows.length > 0)
        .map((g) => (
          <section key={g.label} aria-label={`Hidden ${g.label.toLowerCase()}`}>
            <h3 className="mb-2 text-xs font-bold uppercase tracking-wide text-text-muted">{g.label}</h3>
            <ul className="divide-y divide-border-subtle">
              {g.rows.map((f) => (
                <li key={`${f.targetKind}:${f.targetKey}`} className="flex items-center gap-4 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm text-text-primary">{f.label}</div>
                    {f.sublabel && <div className="truncate text-xs text-text-secondary">{f.sublabel}</div>}
                  </div>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={unhide.isPending}
                    onClick={() => unhide.mutate(f)}
                    aria-label={`Show ${f.label} again`}
                  >
                    Show again
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        ))}
    </div>
  )
}
