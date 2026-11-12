import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Button, ConfirmDialog, Cover, EmptyState, Icon, Skeleton } from '../components/ui'
import { TrackRow } from '../components/ui/TrackRow'
import { PageHero, StickyHeroBar } from '../components/ui/PageHero'
import { MixEditor } from '../components/mixes/MixEditor'
import { coverUrl } from '../lib/libraryApi'
import { reasonText } from '../lib/engineApi'
import { laneLabel, mixTracks, useMix, useMixMutations } from '../lib/mixesApi'
import { usePlayer } from '../lib/playerStore'
import { useToastStore } from '../lib/toastStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import type { Track } from '../lib/types'

function updatedLabel(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  const today = new Date()
  if (d.toDateString() === today.toDateString()) return 'Updated today'
  return `Updated ${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}`
}

export default function Mix() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { data: mix, isLoading, isError } = useMix(id)
  useDocumentTitle(mix?.name ?? 'Mix')
  const playTrackList = usePlayer((s) => s.playTrackList)
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const shuffle = usePlayer((s) => s.shuffle)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const push = useToastStore((s) => s.push)
  const { refresh, remove } = useMixMutations()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [heroGone, setHeroGone] = useState(false)
  const palette = useAlbumPalette(mix?.coverArtId ? coverUrl(mix.coverArtId, 300) : undefined)

  if (isLoading) {
    return (
      <div className="space-y-6" data-testid="mix-skeleton">
        <Skeleton className="h-52 w-52" />
        <Skeleton className="h-10 w-72" />
      </div>
    )
  }
  if (isError || !mix) {
    return <EmptyState icon="music" title="Mix not found" />
  }

  const tracks = mixTracks(mix)
  const context = { kind: 'mix', id: mix.id }
  const play = (i = 0) => tracks.length && playTrackList(tracks, i, context)
  const playShuffled = () => {
    if (!tracks.length) return
    if (!shuffle) toggleShuffle()
    playTrackList(tracks, Math.floor(Math.random() * tracks.length), context)
  }
  const isDaily = mix.kind === 'daily'
  const kindLabel = isDaily ? 'Daily mix' : `Mix · ${laneLabel(mix.recipe.lane)}`

  return (
    <div className="space-y-6">
      <PageHero rgb={palette?.rgb} onVisibleChange={(v) => setHeroGone(!v)}>
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <Cover src={mix.coverArtId ? coverUrl(mix.coverArtId, 300) : undefined} alt={mix.name} size={208} rounded="md" className="shadow-cover flex-none" />
          <div className="min-w-0 pb-1">
            <span className="text-xs font-semibold uppercase tracking-widest text-text-muted">{kindLabel}</span>
            <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate">{mix.name}</h1>
            {mix.description && <p className="mt-2 text-sm text-text-secondary">{mix.description}</p>}
            <p className="mt-1 text-xs text-text-muted">
              {tracks.length} songs · {updatedLabel(mix.generatedAt)}
            </p>
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <Button variant="primary" onClick={() => play(0)} disabled={!tracks.length} aria-label={`Play ${mix.name}`}>
                <Icon name="play" className="w-5 h-5 mr-1" />
                Play
              </Button>
              <Button onClick={playShuffled} disabled={!tracks.length} className="gap-2">
                <Icon name="shuffle" className="w-4 h-4" />
                Shuffle
              </Button>
              <Button
                onClick={() =>
                  refresh.mutate(mix.id, {
                    onSuccess: () => push('Fresh songs in your mix', 'success'),
                    onError: () => push("Couldn't refresh the mix", 'error'),
                  })
                }
                disabled={refresh.isPending}
                className="gap-2"
              >
                <Icon name="retry" className="w-4 h-4" />
                {refresh.isPending ? 'Refreshing…' : 'New songs'}
              </Button>
              {!isDaily && (
                <>
                  <Button variant="ghost" onClick={() => setEditing(true)}>
                    Edit
                  </Button>
                  <Button variant="ghost" onClick={() => setConfirmDelete(true)}>
                    Delete
                  </Button>
                </>
              )}
            </div>
          </div>
        </header>
      </PageHero>
      <StickyHeroBar visible={heroGone} title={mix.name} rgb={palette?.rgb} onPlay={() => play(0)} playDisabled={!tracks.length} playLabel={`Play ${mix.name}`} />

      {tracks.length === 0 ? (
        <EmptyState icon="music" title="Nothing in this mix yet" />
      ) : (
        <div data-testid="mix-tracks">
          {mix.items.map((it, i) => {
            if (!it.owned || !it.track) return null
            const t: Track = it.track
            const idx = tracks.indexOf(t)
            const active = current?.id === t.id
            return (
              <TrackRow
                key={t.id}
                track={t}
                index={i + 1}
                active={active}
                playing={active ? playing : undefined}
                onPlay={() => play(idx)}
                caption={reasonText(it.reason)}
              />
            )
          })}
        </div>
      )}

      {editing && <MixEditor mix={mix} onClose={() => setEditing(false)} />}
      <ConfirmDialog
        open={confirmDelete}
        title={`Delete "${mix.name}"?`}
        body="The mix goes away. The songs stay in your library."
        confirmLabel="Delete mix"
        onCancel={() => setConfirmDelete(false)}
        onConfirm={() => {
          setConfirmDelete(false)
          remove.mutate(mix.id, {
            onSuccess: () => navigate('/'),
            onError: () => push("Couldn't delete the mix", 'error'),
          })
        }}
      />
    </div>
  )
}
