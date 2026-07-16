import { Link, useNavigate } from 'react-router-dom'
import { useLibrarySearch } from '../lib/libraryApi'
import { dedupKey, useEverywhere } from '../lib/everywhereStore'
import { usePlayer } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { postDownload, reqFromResult } from '../lib/downloadApi'
import { useDownloads } from '../lib/downloadStore'
import { useAuthStore } from '../lib/authStore'
import { useToastStore } from '../lib/toastStore'
import { DownloadAction } from '../components/download/DownloadAction'
import { useState } from 'react'
import {
  Segmented,
  TrackRow,
  MediaCard,
  Badge,
  Icon,
  Button,
  EmptyState,
  Skeleton,
} from '../components/ui'
import type { SourceStatus } from '../lib/everywhereStore'
import type { ExternalResult, EnvelopeStatus, Track } from '../lib/types'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { useDebouncedValue } from '../lib/useDebouncedValue'
import { usePendingPlay } from '../lib/pendingPlayStore'

// ── Helpers ──────────────────────────────────────────────────────────────────

type ResultFilter = 'all' | 'track' | 'playlist' | 'album' | 'artist'

const RESULT_FILTERS: { value: ResultFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'track', label: 'Songs' },
  { value: 'playlist', label: 'Playlists' },
  { value: 'album', label: 'Albums' },
  { value: 'artist', label: 'Artists' },
]

function sourceTone(status: EnvelopeStatus): 'success' | 'warning' | 'error' {
  if (status === 'ok') return 'success'
  if (status === 'timeout') return 'warning'
  return 'error'
}

function sourceName(s: SourceStatus): string {
  return s.source.charAt(0).toUpperCase() + s.source.slice(1)
}

function sourceLabel(s: SourceStatus): string {
  const name = sourceName(s)
  if (s.status === 'ok') return name
  if (s.status === 'timeout') return `${name} timed out`
  return `${name} error`
}

/** Minimal library Track synthesised from an external result + matched library id. */
function trackFromMatch(r: ExternalResult, libraryTrackId: string): Track {
  return {
    id: libraryTrackId,
    title: r.title,
    albumId: '',
    album: r.album,
    artistId: '',
    artist: r.artist,
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

// ── Source chips ──────────────────────────────────────────────────────────────

function SourceChipsRow({
  sources,
  hiddenSources,
  onToggle,
}: {
  sources: SourceStatus[]
  hiddenSources: ReadonlySet<string>
  onToggle: (source: string) => void
}) {
  if (sources.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-2" aria-label="Source status">
      {sources.map((s) => (
        <button
          key={s.source}
          type="button"
          aria-pressed={!hiddenSources.has(s.source)}
          aria-label={`${hiddenSources.has(s.source) ? 'Show' : 'Hide'} ${sourceName(s)} results`}
          onClick={() => onToggle(s.source)}
          className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Badge key={s.source} kind="status" tone={sourceTone(s.status)}>
            {s.status === 'ok' ? (
              <>
                {sourceName(s)}
                {!hiddenSources.has(s.source) && <Icon name="check" className="ml-1 text-xs" aria-hidden="true" />}
              </>
            ) : (
              sourceLabel(s)
            )}
          </Badge>
        </button>
      ))}
    </div>
  )
}

// ── Section header ────────────────────────────────────────────────────────────

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="mb-2 text-sm font-bold uppercase tracking-wider text-text-muted">
      {children}
    </h2>
  )
}

// ── Library-mode skeleton ─────────────────────────────────────────────────────

function TrackSkeletons() {
  return (
    <div className="space-y-1" aria-busy="true" aria-label="Loading tracks">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-12 w-full" />
      ))}
    </div>
  )
}

// ── Main component ─────────────────────────────────────────────────────────────

export default function Search() {
  useDocumentTitle('Search')
  const q = useSearch((s) => s.query)
  const setQ = useSearch((s) => s.setQuery)
  const debouncedQ = useDebouncedValue(q, 400)

  const playTrackList = usePlayer((s) => s.playTrackList)
  const currentTrackId = usePlayer((s) => s.current?.id)
  const navigate = useNavigate()
  // The bulk album "Download all" is a direct download — gated on auto_approve.
  // Search has no bulk-request path; a user without auto_approve simply sees no
  // bulk download (per-item acquisition on the track rows is unaffected).
  const canAutoApprove = useAuthStore((s) => s.can('auto_approve'))
  const [resultFilter, setResultFilter] = useState<ResultFilter>('all')
  const [hiddenSources, setHiddenSources] = useState<Set<string>>(() => new Set())

  // Library mode: TanStack Query REST call
  const lib = useLibrarySearch(q)

  // Everywhere mode: SSE stream via reducer (transport stays entirely in useEverywhere)
  const everywhere = useEverywhere(debouncedQ, 'track', debouncedQ.trim() !== '')

  const libTracks = lib.data?.tracks ?? []
  const libAlbums = lib.data?.albums ?? []
  const libArtists = lib.data?.artists ?? []
  const isVisibleSource = (source: string) => !hiddenSources.has(source)
  const tracks = everywhere.tracks.filter((result) => isVisibleSource(result.source))
  const albums = everywhere.albums.filter((result) => isVisibleSource(result.source))
  const artists = everywhere.artists.filter((result) => isVisibleSource(result.source))
  const playlists = everywhere.playlists.filter((result) => isVisibleSource(result.source))
  const libraryTrackKeys = new Set(libTracks.map((track) => dedupKey({ isrc: track.isrc, artist: track.artist, title: track.title } as ExternalResult)))
  const externalTracks = tracks.filter((result) => result.match?.status !== 'in_library' && !libraryTrackKeys.has(dedupKey(result)))

  function toggleSource(source: string) {
    setHiddenSources((current) => {
      const next = new Set(current)
      if (next.has(source)) next.delete(source)
      else next.add(source)
      return next
    })
  }

  // ── Empty-query prompt ──────────────────────────────────────────────────────
  if (q.trim() === '') {
    return (
      <div className="space-y-6">
        {/* Mobile-only input — the TopBar has no search bar on mobile. */}
        <MobileSearchInput q={q} onChange={setQ} />

        <h1 className="text-xl font-extrabold text-text-primary">Search</h1>

        <div className="py-12">
          <EmptyState
            icon="search"
            title="Find your music"
            hint="Type an artist, album, or track name to search your library or discover new music everywhere."
          />
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-8">
      {/* Mobile-only input — the TopBar has no search bar on mobile. */}
      <MobileSearchInput q={q} onChange={setQ} />

      <div>
        <h1 className="truncate text-xl font-extrabold text-text-primary">
          Results for &ldquo;{q}&rdquo;
        </h1>
      </div>

      {!lib.isFetching && libTracks.length === 0 && libAlbums.length === 0 && libArtists.length === 0 && lib.isFetched && everywhere.status === 'done' && externalTracks.length === 0 && albums.length === 0 && artists.length === 0 && playlists.length === 0 && (
        <EmptyState
          icon="search"
          title="No results"
          hint={`Nothing matches "${q}" in your library or connected sources.`}
        />
      )}

      {/* Source chips */}
      <SourceChipsRow sources={everywhere.sources} hiddenSources={hiddenSources} onToggle={toggleSource} />

      <div className="overflow-x-auto pb-1">
        <Segmented options={RESULT_FILTERS} value={resultFilter} onChange={setResultFilter} />
      </div>

      {/* Streaming hint — shows while at least one envelope is in flight */}
      {everywhere.status === 'streaming' && (
        <p className="text-xs text-text-muted" aria-live="polite">
          Searching sources...
        </p>
      )}

      {/* Songs — library rows first, then external rows, in one section */}
      {(resultFilter === 'all' || resultFilter === 'track') &&
        (lib.isFetching || libTracks.length > 0 || externalTracks.length > 0 || everywhere.status === 'streaming') && (
        <section aria-label="Songs">
          <SectionHeading>Songs</SectionHeading>
          {lib.isFetching ? (
            <TrackSkeletons />
          ) : libTracks.length === 0 && externalTracks.length === 0 && everywhere.status !== 'streaming' ? (
            <p className="text-sm text-text-muted">No tracks found.</p>
          ) : (
            <div className="space-y-0.5">
              {libTracks.map((t, i) => (
                <TrackRow
                  key={t.id}
                  track={t}
                  index={i}
                  active={currentTrackId === t.id}
                  onPlay={() => playTrackList(libTracks, i)}
                />
              ))}
              {externalTracks.map((r) => {
                const matchedId =
                  (r.match?.status === 'in_library' && r.match.libraryTrackId) || ''
                const syntheticTrack = matchedId ? trackFromMatch(r, matchedId) : null

                // For display in TrackRow we need a Track shape. We always render
                // a synthetic Track — the right slot carries the DownloadAction.
                const displayTrack: Track = syntheticTrack ?? {
                  id: `${r.source}:${r.externalId}`,
                  title: r.title,
                  albumId: '',
                  album: r.album,
                  artistId: '',
                  artist: r.artist,
                  coverArtId: r.coverArtId ?? '',
                  trackNumber: 0,
                  discNumber: 0,
                  durationMs: r.durationMs,
                  bitRate: 0,
                  suffix: '',
                  contentType: '',
                  isrc: r.isrc,
                }

                // For non-library results, link artist/album to external pages if IDs are present.
                const artistNode = !syntheticTrack && r.artistExternalId ? (
                  <Link
                    to={`/artist/${r.source}/${r.artistExternalId}`}
                    onClick={(e) => e.stopPropagation()}
                    onDoubleClick={(e) => e.stopPropagation()}
                    className="hover:underline"
                  >
                    {r.artist}
                  </Link>
                ) : undefined
                const albumNode = !syntheticTrack && r.albumExternalId ? (
                  <Link
                    to={`/album/${r.source}/${r.albumExternalId}`}
                    onClick={(e) => e.stopPropagation()}
                    onDoubleClick={(e) => e.stopPropagation()}
                    className="hover:underline"
                  >
                    {r.album}
                  </Link>
                ) : undefined

                return (
                  <TrackRow
                    key={`${r.source}:${r.externalId}`}
                    track={displayTrack}
                    coverSrc={r.coverUrl || undefined}
                    rightWidth="8.5rem"
                    active={!!matchedId && currentTrackId === matchedId}
                    artistNode={artistNode}
                    albumNode={albumNode}
                    onPlay={() => {
                      if (syntheticTrack) {
                        playTrackList([syntheticTrack], 0)
                      } else {
                        // Not in your library yet — clicking the song downloads it
                        // (server picks the downloader via the fallback chain).
                        const wasPlaying = usePlayer.getState().current !== null
                        postDownload({ ...reqFromResult(r), playWhenReady: true })
                          .then((j) => {
                            useDownloads.getState().upsert(j)
                            usePendingPlay.getState().begin({ jobId: j.id, title: r.title, artist: r.artist, coverArtId: r.coverArtId })
                            // The pending-play indicator in PlayerBar only shows when
                            // nothing is playing — if the user is mid-listen, a toast
                            // is their only feedback that the download was queued.
                            if (wasPlaying) {
                              useToastStore.getState().push(`Downloading "${r.title}" — it'll be added to your queue`, 'info')
                            }
                          })
                          .catch(() => {})
                      }
                    }}
                    right={
                      <DownloadAction
                        result={r}
                        onPlay={(libraryTrackId) => {
                          playTrackList([trackFromMatch(r, libraryTrackId)], 0)
                        }}
                      />
                    }
                  />
                )
              })}
            </div>
          )}
        </section>
      )}

      {/* Albums — library cards first, then external cards, in one grid */}
      {(resultFilter === 'all' || resultFilter === 'album') && (libAlbums.length > 0 || albums.length > 0) && (
        <section aria-label="Albums">
          <SectionHeading>Albums</SectionHeading>
          {/* TODO(phase-6): partial N-of-M needs external album tracks + matching */}
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {libAlbums.map((al) => (
              <MediaCard
                key={al.id}
                title={al.name}
                subtitle={al.artist}
                coverId={al.coverArtId}
                onClick={() => navigate(`/album/library/${al.id}`)}
              />
            ))}
            {albums.map((a) => (
              <MediaCard
                key={`${a.source}:${a.externalId}`}
                title={a.title}
                subtitle={a.artist}
                onClick={() => navigate(`/album/${a.source}/${a.externalId}`)}
                badge={
                  a.match?.status === 'in_library' ? (
                    <Badge kind="in-library">
                      In Library
                    </Badge>
                  ) : canAutoApprove ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      aria-label={`Download all of ${a.title}`}
                      onClick={(e) => {
                        e.stopPropagation()
                        void postDownload({
                          source: a.source,
                          externalId: a.externalId,
                          artist: a.artist,
                          title: a.title,
                          album: a.title,
                        }).then((j) => useDownloads.getState().upsert(j))
                      }}
                    >
                      <Icon name="dl" className="text-xs" />
                      Download all
                    </Button>
                  ) : undefined
                }
              />
            ))}
          </div>
        </section>
      )}

      {/* Artists — library cards first, then external cards, in one grid */}
      {(resultFilter === 'all' || resultFilter === 'artist') && (libArtists.length > 0 || artists.length > 0) && (
        <section aria-label="Artists">
          <SectionHeading>Artists</SectionHeading>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
            {libArtists.map((ar) => (
              <MediaCard
                key={ar.id}
                title={ar.name}
                coverId={ar.coverArtId}
                rounded="full"
                onClick={() => navigate(`/artist/library/${ar.id}`)}
              />
            ))}
            {artists.map((r) => (
              <MediaCard
                key={`${r.source}:${r.externalId}`}
                title={r.title}
                rounded="full"
                onClick={() => navigate(`/artist/${r.source}/${r.externalId}`)}
              />
            ))}
          </div>
        </section>
      )}

      {(resultFilter === 'all' || resultFilter === 'playlist') && playlists.length > 0 && (
        <section aria-label="Playlists">
          <SectionHeading>Playlists</SectionHeading>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {playlists.map((playlist) => (
              <MediaCard
                key={`${playlist.source}:${playlist.externalId}`}
                title={playlist.title}
                subtitle={playlist.artist || 'Spotify playlist'}
                coverSrc={playlist.coverUrl || undefined}
                onClick={() => navigate(`/playlist/${playlist.source}/${playlist.externalId}`)}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}

// ── Mobile-only search input ──────────────────────────────────────────────────
//
// On desktop the TopBar typeahead IS the input, so the page renders no input of
// its own. On mobile the TopBar hides its search bar (it's `hidden md:flex`),
// so the Search page carries the input — wrapped in `md:hidden`. The placeholder
// stays mode-conditional. The scope toggle now lives in the results header.

interface MobileSearchInputProps {
  q: string
  onChange: (v: string) => void
}

function MobileSearchInput({ q, onChange }: MobileSearchInputProps) {
  const placeholder = 'Search your library — or everywhere'
  return (
    <div className="md:hidden">
      <label className="sr-only" htmlFor="search-input">
        Search
      </label>
      <div className="relative">
        <Icon
          name="search"
          className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-text-secondary"
          aria-hidden="true"
        />
        <input
          id="search-input"
          autoFocus
          value={q}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          className="w-full rounded-full bg-raised py-3 pl-11 pr-4 text-sm text-text-primary outline-none ring-1 ring-border-subtle placeholder:text-text-muted focus-visible:ring-2 focus-visible:ring-accent"
        />
      </div>
    </div>
  )
}
