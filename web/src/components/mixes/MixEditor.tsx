import { useEffect, useId, useState } from 'react'
import { createPortal } from 'react-dom'
import { Button, Icon, Select } from '../ui'
import { useArtists } from '../../lib/libraryApi'
import { useGenres, useMixMutations, type Mix, type MixRecipe, type MixRefresh } from '../../lib/mixesApi'
import type { EngineSeed, Lane } from '../../lib/engineApi'

const STYLES: { value: Lane; label: string }[] = [
  { value: '', label: 'Seeds plus similar music' },
  { value: 'deep-cuts', label: 'Deep cuts' },
  { value: 'sounds-like', label: 'Sounds like (without the seeds)' },
  { value: 'new-to-you', label: 'New to you' },
  { value: 'on-repeat', label: 'On repeat' },
  { value: 'fresh-finds', label: 'Fresh finds' },
  { value: 'throwback', label: 'Throwback (pick years)' },
]
const SIZES = ['20', '30', '50', '100']
const REFRESH: { value: MixRefresh; label: string }[] = [
  { value: 'daily', label: 'New songs every day' },
  { value: 'weekly', label: 'New songs every week' },
  { value: 'manual', label: 'Only when I refresh it' },
]

interface Props {
  /** The mix to edit; omitted to create one. */
  mix?: Mix
  onClose: () => void
  /** Called with the saved mix. */
  onSaved?: (m: Mix) => void
}

/** Create or edit a custom mix: a name, seeds (artists, genres), a style, a length and a refresh schedule. */
export function MixEditor({ mix, onClose, onSaved }: Props) {
  const titleId = useId()
  const listId = useId()
  const { data: artists } = useArtists()
  const { data: genres } = useGenres()
  const { create, update } = useMixMutations()

  const [name, setName] = useState(mix?.name ?? '')
  const [seeds, setSeeds] = useState<EngineSeed[]>(mix?.recipe.seeds.filter((s) => s.kind !== 'taste') ?? [])
  const [lane, setLane] = useState<Lane>(mix?.recipe.lane ?? '')
  const [fromYear, setFromYear] = useState(String(mix?.recipe.fromYear ?? 2000))
  const [toYear, setToYear] = useState(String(mix?.recipe.toYear ?? 2009))
  const [size, setSize] = useState(String(mix?.recipe.size ?? 30))
  const [refresh, setRefresh] = useState<MixRefresh>(mix?.recipe.refresh ?? 'daily')
  const [addKind, setAddKind] = useState<'artist' | 'genre'>('artist')
  const [addText, setAddText] = useState('')
  const [error, setError] = useState<string | null>(null)
  const busy = create.isPending || update.isPending

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  const genreLabel = (key?: string) => genres?.find((g) => g.key === key)?.label ?? key ?? ''
  const seedLabel = (s: EngineSeed) => (s.kind === 'genre' ? genreLabel(s.id) : s.artist ?? s.title ?? s.id ?? '')

  function addSeed() {
    const text = addText.trim()
    if (!text || seeds.length >= 10) return
    let seed: EngineSeed
    if (addKind === 'genre') {
      const g = genres?.find((x) => x.label.toLowerCase() === text.toLowerCase() || x.key === text.toLowerCase())
      if (!g) {
        setError(`No "${text}" genre in your library.`)
        return
      }
      seed = { kind: 'genre', id: g.key }
    } else {
      const a = artists?.find((x) => x.name.toLowerCase() === text.toLowerCase())
      seed = { kind: 'artist', artist: a?.name ?? text }
    }
    if (seeds.some((s) => s.kind === seed.kind && seedLabel(s).toLowerCase() === seedLabel(seed).toLowerCase())) {
      setAddText('')
      return
    }
    setSeeds([...seeds, seed])
    setAddText('')
    setError(null)
  }

  function save() {
    const n = name.trim()
    if (!n) {
      setError('Give the mix a name.')
      return
    }
    const recipe: MixRecipe = { seeds, lane, size: Number(size), refresh }
    if (lane === 'throwback') {
      const f = Number(fromYear)
      const t = Number(toYear)
      if (!f || !t || t < f) {
        setError('Pick a period: the first year, then the last.')
        return
      }
      recipe.fromYear = f
      recipe.toYear = t
    }
    setError(null)
    const done = {
      onSuccess: (m: Mix) => {
        onSaved?.(m)
        onClose()
      },
      onError: () => setError("Couldn't save the mix. Check the seeds are in your library."),
    }
    if (mix) update.mutate({ id: mix.id, name: n, recipe }, done)
    else create.mutate({ name: n, recipe }, done)
  }

  const options = addKind === 'genre' ? (genres ?? []).map((g) => g.label) : (artists ?? []).map((a) => a.name)

  return createPortal(
    <>
      <div className="fixed inset-0 z-40 bg-canvas/80 backdrop-blur-sm animate-fade-in" aria-hidden="true" onClick={onClose} />
      <div role="dialog" aria-modal="true" aria-labelledby={titleId} className="pointer-events-none fixed inset-0 z-50 flex items-center justify-center p-4">
        <div className="pointer-events-auto w-full max-w-md space-y-4 rounded-xl border border-border-subtle bg-raised p-6 shadow-pop animate-scale-in">
          <h2 id={titleId} className="text-lg font-extrabold tracking-tight text-text-primary">
            {mix ? 'Edit mix' : 'New mix'}
          </h2>

          <label className="block space-y-1">
            <span className="text-xs font-semibold text-text-secondary">Name</span>
            <input
              value={name}
              maxLength={100}
              onChange={(e) => setName(e.target.value)}
              placeholder="Late night"
              aria-label="Mix name"
              className="w-full rounded-lg border border-border-subtle bg-surface px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
          </label>

          <div className="space-y-2">
            <span className="text-xs font-semibold text-text-secondary">Built from</span>
            <div className="flex flex-wrap gap-2" aria-label="Seeds">
              {seeds.length === 0 && <span className="text-sm text-text-muted">Your taste (add artists or genres to steer it)</span>}
              {seeds.map((s, i) => (
                <span key={`${s.kind}:${seedLabel(s)}`} className="inline-flex items-center gap-1 rounded-full bg-surface px-3 py-1 text-sm text-text-primary">
                  {seedLabel(s)}
                  <span className="text-xs text-text-muted">{s.kind === 'genre' ? 'genre' : ''}</span>
                  <button
                    type="button"
                    aria-label={`Remove ${seedLabel(s)}`}
                    onClick={() => setSeeds(seeds.filter((_, j) => j !== i))}
                    className="text-text-muted hover:text-text-primary"
                  >
                    <Icon name="x" className="w-3.5 h-3.5" />
                  </button>
                </span>
              ))}
            </div>
            <div className="flex items-center gap-2">
              <Select
                label="Seed type"
                value={addKind}
                onChange={(v) => setAddKind(v as 'artist' | 'genre')}
                options={[
                  { value: 'artist', label: 'Artist' },
                  { value: 'genre', label: 'Genre' },
                ]}
              />
              <input
                value={addText}
                list={listId}
                onChange={(e) => setAddText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    addSeed()
                  }
                }}
                placeholder={addKind === 'genre' ? 'Jazz' : 'Bonobo'}
                aria-label={addKind === 'genre' ? 'Genre to add' : 'Artist to add'}
                className="min-w-0 flex-1 rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
              <datalist id={listId}>
                {options.slice(0, 500).map((o) => (
                  <option key={o} value={o} />
                ))}
              </datalist>
              <Button size="sm" onClick={addSeed} disabled={!addText.trim() || seeds.length >= 10}>
                Add
              </Button>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="col-span-2 space-y-1">
              <span className="block text-xs font-semibold text-text-secondary">Style</span>
              <Select label="Style" value={lane} onChange={(v) => setLane(v as Lane)} options={STYLES} />
            </label>
            {lane === 'throwback' && (
              <>
                <label className="space-y-1">
                  <span className="block text-xs font-semibold text-text-secondary">From</span>
                  <input type="number" aria-label="From year" value={fromYear} onChange={(e) => setFromYear(e.target.value)} className="w-full rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-sm text-text-primary" />
                </label>
                <label className="space-y-1">
                  <span className="block text-xs font-semibold text-text-secondary">To</span>
                  <input type="number" aria-label="To year" value={toYear} onChange={(e) => setToYear(e.target.value)} className="w-full rounded-lg border border-border-subtle bg-surface px-3 py-1.5 text-sm text-text-primary" />
                </label>
              </>
            )}
            <label className="space-y-1">
              <span className="block text-xs font-semibold text-text-secondary">Length</span>
              <Select label="Length" value={size} onChange={setSize} options={SIZES.map((s) => ({ value: s, label: `${s} songs` }))} />
            </label>
            <label className="space-y-1">
              <span className="block text-xs font-semibold text-text-secondary">Refresh</span>
              <Select label="Refresh" value={refresh} onChange={(v) => setRefresh(v as MixRefresh)} options={REFRESH} />
            </label>
          </div>

          {error && (
            <p role="alert" className="text-sm text-error">
              {error}
            </p>
          )}
          <div className="flex justify-end gap-2 pt-1">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button variant="primary" onClick={save} disabled={busy}>
              {busy ? 'Building…' : mix ? 'Save' : 'Create mix'}
            </Button>
          </div>
        </div>
      </div>
    </>,
    document.body,
  )
}
