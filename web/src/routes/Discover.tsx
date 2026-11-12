import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Button, EmptyState, Icon, Skeleton } from '../components/ui'
import { TrackRow } from '../components/ui/TrackRow'
import { PageHero, StickyHeroBar } from '../components/ui/PageHero'
import { DownloadAction } from '../components/download/DownloadAction'
import { DiscoverArt } from '../components/mixes/DiscoverArt'
import { reasonText } from '../lib/engineApi'
import { KEEP_RULES, discoverTracks, refreshLabel, useDiscover, type DiscoverItem } from '../lib/discoverApi'
import { usePlayer } from '../lib/playerStore'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import type { ExternalResult, Track } from '../lib/types'

function externalTrack(r: ExternalResult): Track {
  return {
    id: `${r.source}:${r.externalId}`,
    title: r.title,
    artist: r.artist,
    artistId: '',
    album: r.album,
    albumId: '',
    coverArtId: r.coverArtId ?? '',
    trackNumber: 0,
    discNumber: 0,
    durationMs: r.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
    isrc: r.isrc,
  }
}

export default function Discover() {
  useDocumentTitle('Discover Weekly')
  const { data: week, isLoading, isError } = useDiscover()
  const playTrackList = usePlayer((s) => s.playTrackList)
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const shuffle = usePlayer((s) => s.shuffle)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const [heroGone, setHeroGone] = useState(false)

  if (isLoading) {
    return (
      <div className="space-y-6" data-testid="discover-skeleton">
        <Skeleton className="h-52 w-52" />
        <Skeleton className="h-10 w-72" />
      </div>
    )
  }
  if (isError || !week) {
    return <EmptyState icon="compass" title="Couldn't load Discover Weekly" hint="Try again in a moment." />
  }
  if (!week.prefs.enabled) {
    return (
      <EmptyState
        icon="compass"
        title="Discover Weekly is off"
        hint="Turn it on in Settings, under Recommendations."
        action={
          <Link to="/settings/recommendations" className="text-sm font-semibold text-accent hover:underline">
            Open settings
          </Link>
        }
      />
    )
  }

  const tracks = discoverTracks(week)
  const context = { kind: 'discover', id: week.week }
  const play = (i = 0) => tracks.length && playTrackList(tracks, i, context)
  const playShuffled = () => {
    if (!tracks.length) return
    if (!shuffle) toggleShuffle()
    playTrackList(tracks, Math.floor(Math.random() * tracks.length), context)
  }
  const fetching = week.items.filter((i) => !i.owned && i.state === 'downloading' && i.jobStatus !== 'failed').length
  const keep = KEEP_RULES.find((k) => k.value === week.prefs.keepRule)
  const items = week.items.filter((i) => i.state !== 'removed' && i.state !== 'expired')

  let status = `${tracks.length} of ${items.length} songs ready`
  if (fetching) status += ` · fetching ${fetching}`
  status += ` · ${refreshLabel(week.endsAt)}`

  return (
    <div className="space-y-6">
      <PageHero onVisibleChange={(v) => setHeroGone(!v)}>
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <DiscoverArt size={208} />
          <div className="min-w-0 pb-1">
            <span className="text-xs font-semibold uppercase tracking-widest text-text-muted">Made for you</span>
            <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary">Discover Weekly</h1>
            <p className="mt-2 text-sm text-text-secondary" data-testid="discover-blurb">
              {items.length > 0 && !items.some((i) => i.reason.kind === 'discovery' || (!i.owned && i.external))
                ? 'Picked from your library for now. New music shows up here once Reverb can look up artists like the ones you love.'
                : 'New music picked from artists you love. Most of it isn’t in your library yet.'}
            </p>
            <p className="mt-1 text-xs text-text-muted" data-testid="discover-status">
              {status}
            </p>
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <Button variant="primary" onClick={() => play(0)} disabled={!tracks.length} aria-label="Play Discover Weekly">
                <Icon name="play" className="w-5 h-5 mr-1" />
                Play
              </Button>
              <Button onClick={playShuffled} disabled={!tracks.length} className="gap-2">
                <Icon name="shuffle" className="w-4 h-4" />
                Shuffle
              </Button>
            </div>
          </div>
        </header>
      </PageHero>
      <StickyHeroBar visible={heroGone} title="Discover Weekly" onPlay={() => play(0)} playDisabled={!tracks.length} playLabel="Play Discover Weekly" />

      {items.length === 0 ? (
        <EmptyState
          icon="compass"
          title="Nothing to discover yet"
          hint="Play some music first. Discover Weekly learns from what you listen to and builds a new list every Monday."
        />
      ) : (
        <div data-testid="discover-tracks">
          {items.map((it, i) => (
            <DiscoverRow
              key={it.id}
              item={it}
              index={i}
              currentId={current?.id}
              playing={playing}
              onPlayOwned={(t) => play(tracks.indexOf(t))}
              onPlayLanded={(t) => playTrackList([t], 0, context)}
            />
          ))}
        </div>
      )}

      {week.downloads && keep && (
        <p className="text-xs text-text-muted" data-testid="discover-keep-note">
          {keep.hint}{' '}
          <Link to="/settings/recommendations" className="font-semibold text-text-secondary hover:underline">
            Change this
          </Link>
        </p>
      )}
      {!week.downloads && hasExternal(week.items) && (
        <p className="text-xs text-text-muted">Songs that aren&apos;t in your library yet can be downloaded or requested one by one.</p>
      )}
    </div>
  )
}

function hasExternal(items: DiscoverItem[]) {
  return items.some((i) => !i.owned && i.external)
}

interface RowProps {
  item: DiscoverItem
  index: number
  currentId?: string
  playing: boolean
  onPlayOwned: (t: Track) => void
  onPlayLanded: (t: Track) => void
}

function DiscoverRow({ item, index, currentId, playing, onPlayOwned, onPlayLanded }: RowProps) {
  const caption = reasonText(item.reason)
  if (item.owned && item.track) {
    const t = item.track
    const active = currentId === t.id
    return <TrackRow track={t} index={index} active={active} playing={active ? playing : undefined} onPlay={() => onPlayOwned(t)} caption={caption} />
  }
  if (!item.external) return null
  const r = item.external
  return (
    <TrackRow
      track={externalTrack(r)}
      index={index}
      coverSrc={r.coverUrl || undefined}
      rightWidth="8.5rem"
      caption={caption}
      onPlay={() => {}}
      right={
        <DownloadAction
          result={r}
          onPlay={(libraryTrackId) => onPlayLanded({ ...externalTrack(r), id: libraryTrackId })}
        />
      }
    />
  )
}

