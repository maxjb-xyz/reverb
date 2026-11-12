import { Button, EmptyState } from '../ui'
import { useTasteFeedback, useUnhide } from '../../lib/tasteApi'

/**
 * Settings → Recommendations. Lists the songs and artists the user said "not
 * for me" to, with a way to bring each back. Later phases add mix and
 * Discover settings here.
 */
export function RecommendationsSection() {
  const { data, isLoading, isError } = useTasteFeedback()
  const unhide = useUnhide()
  const items = data ?? []
  const artists = items.filter((f) => f.targetKind === 'artist')
  const tracks = items.filter((f) => f.targetKind === 'track')

  return (
    <div className="space-y-6">
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
