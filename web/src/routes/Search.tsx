import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { adapterDisplayName } from '../lib/displayNames'
import { useLibrarySearch } from '../lib/libraryApi'
import { dedupKey, useEverywhere } from '../lib/everywhereStore'
import { usePlayer } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { postDownload, reqFromResult } from '../lib/downloadApi'
import { useDownloads } from '../lib/downloadStore'
import { useAuthStore } from '../lib/authStore'
import { useToastStore } from '../lib/toastStore'
import { DownloadAction } from '../components/download/DownloadAction'
import { useEffect, useRef, useState } from 'react'
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
import type { DownloadJob, ExternalResult, EnvelopeStatus, Track } from '../lib/types'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { useDebouncedValue } from '../lib/useDebouncedValue'
import { usePendingPlay } from '../lib/pendingPlayStore'
import { PageHeader } from '../components/ui/PageHeader'

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
  return adapterDisplayName(s.source)
}

function sourceLabel(s: SourceStatus): string {
  const name = sourceName(s)
  if (s.status === 'ok') return name
  if (s.status === 'timeout') return `${name} timed out`
  return `${name} unavailable`
}

/** Minimal library Track synthesised from an external result + matched library id. */
function trackFromMatch(r: ExternalResult, libraryTrackId: string, coverArtId?: string): Track {
  return {
    id: libraryTrackId,
    title: r.title,
    albumId: '',
    album: r.album,
    artistId: '',
    artist: r.artist,
    coverArtId: coverArtId || r.coverArtId || '',
    trackNumber: 0,
    discNumber: 0,
    durationMs: r.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
    isrc: r.isrc,
  }
}

// The finished download of this exact result, if any. A result's `match` is a
// snapshot from when the search ran, so a song downloaded from this page is only
// known to be owned through its job. Prefers a job already linked to a library
// track over one still waiting for the library scan to pick the file up.
function completedJobFor(jobs: Record<string, DownloadJob>, r: ExternalResult): DownloadJob | undefined {
  let pending: DownloadJob | undefined
  for (const j of Object.values(jobs)) {
    if (j.status !== 'completed' || j.source !== r.source || j.externalId !== r.externalId) continue
    if (j.libraryTrackId) return j
    pending ??= j
  }
  return pending
}

// ── Source chips ──────────────────────────────────────────────────────────────

function SourceChipsRow({
  sources,
  hiddenSources,
  onToggle,
  onRetry,
}: {
  sources: SourceStatus[]
  hiddenSources: ReadonlySet<string>
  onToggle: (source: string) => void
  onRetry: () => void
}) {
  if (sources.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-2" aria-label="Source status">
      {sources.map((s) =>
        s.status === 'ok' ? (
          <button
            key={s.source}
            type="button"
            aria-pressed={!hiddenSources.has(s.source)}
            aria-label={`${hiddenSources.has(s.source) ? 'Show' : 'Hide'} ${sourceName(s)} results`}
            onClick={() => onToggle(s.source)}
            className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Badge kind="status" tone="success">
              {sourceName(s)}
              {!hiddenSources.has(s.source) && <Icon name="check" className="ml-1 text-xs" aria-hidden="true" />}
            </Badge>
          </button>
        ) : (
          // A source that failed has no results to show or hide, so it is a
          // plain status with a way to try again rather than a toggle.
          <Badge key={s.source} kind="status" tone={sourceTone(s.status)}>
            {sourceLabel(s)}
          </Badge>
        ),
      )}
      {sources.some((s) => s.status !== 'ok') && (
        <button
          type="button"
          onClick={onRetry}
          className="rounded-full px-2 py-0.5 text-xs font-semibold text-text-secondary underline-offset-2 hover:text-text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          Retry
        </button>
      )}
    </div>
  )
}

// ── Section header ────────────────────────────────────────────────────────────

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="mb-2 text-base font-bold text-text-primary">
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

  // Keep the query in the URL (?q=) so a search can be bookmarked, shared and
  // restored by the back button. On first mount the URL seeds the shared store;
  // after that the store (typed in either search box) drives the URL. `replace`
  // keeps keystrokes out of the history stack.
  const [params, setParams] = useSearchParams()
  const urlSeeded = useRef(false)
  useEffect(() => {
    const urlQ = params.get('q') ?? ''
    if (!urlSeeded.current) {
      urlSeeded.current = true
      if (urlQ && urlQ !== q) {
        setQ(urlQ)
        return
      }
    }
    if (q !== urlQ) setParams(q ? { q } : {}, { replace: true })
  }, [q, params, setQ, setParams])

  const playTrackList = usePlayer((s) => s.playTrackList)
  const currentTrackId = usePlayer((s) => s.current?.id)
  const downloadJobs = useDownloads((s) => s.jobs)
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
  const [attempt, setAttempt] = useState(0)
  const everywhere = useEverywhere(debouncedQ, 'track', debouncedQ.trim() !== '', attempt)

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

        <PageHeader title="Search" />

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

      <PageHeader title={<>Results for &ldquo;{q}&rdquo;</>} />

      {!lib.isFetching && libTracks.length === 0 && libAlbums.length === 0 && libArtists.length === 0 && lib.isFetched && everywhere.status === 'done' && externalTracks.length === 0 && albums.length === 0 && artists.length === 0 && playlists.length === 0 && (
        <EmptyState
          icon="search"
          title="No results"
          hint={`Nothing matches "${q}" in your library or connected sources.`}
        />
      )}

      {/* Source chips */}
      <SourceChipsRow sources={everywhere.sources} hiddenSources={hiddenSources} onToggle={toggleSource} onRetry={() => setAttempt((n) => n + 1)} />

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
            <div className="rvb-stagger space-y-0.5">
              {libTracks.map((t, i) => (
                <TrackRow
                  key={t.id}
                  track={t}
                  active={currentTrackId === t.id}
                  onPlay={() => playTrackList(libTracks, i)}
                />
              ))}
              {externalTracks.map((r) => {
                const doneJob = completedJobFor(downloadJobs, r)
                const matchedId =
                  (r.match?.status === 'in_library' && r.match.libraryTrackId) ||
                  doneJob?.libraryTrackId ||
                  ''
                const syntheticTrack = matchedId ? trackFromMatch(r, matchedId, doneJob?.coverArtId) : null

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
                      } else if (doneJob) {
                        // Downloaded, but the library hasn't indexed the file yet.
                        // Downloading it again would only make a duplicate.
                        useToastStore.getState().push(`"${r.title}" is still being added to your library`, 'info')
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
                          playTrackList([trackFromMatch(r, libraryTrackId, doneJob?.coverArtId)], 0)
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
          <div className="rvb-stagger grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
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
          <div className="rvb-stagger grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
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
          <div className="rvb-stagger grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
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
