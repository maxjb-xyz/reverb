import { Link, useNavigate } from 'react-router-dom'
import { useLibrarySearch } from '../lib/libraryApi'
import { useEverywhere } from '../lib/everywhereStore'
import { usePlayer } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { postDownload, reqFromResult } from '../lib/downloadApi'
import { useDownloads } from '../lib/downloadStore'
import { DownloadAction } from '../components/download/DownloadAction'
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

// ── Helpers ──────────────────────────────────────────────────────────────────

type Mode = 'library' | 'everywhere'

const MODE_OPTIONS: { value: Mode; label: string }[] = [
  { value: 'library', label: 'My Library' },
  { value: 'everywhere', label: 'Everywhere' },
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

function SourceChipsRow({ sources }: { sources: SourceStatus[] }) {
  if (sources.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-2" aria-label="Source status">
      {sources.map((s) => (
        <Badge key={s.source} kind="status" tone={sourceTone(s.status)}>
          {s.status === 'ok' ? (
            <>
              {sourceName(s)}
              <Icon name="check" className="ml-1 text-xs" aria-hidden="true" />
            </>
          ) : (
            sourceLabel(s)
          )}
        </Badge>
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
  const q = useSearch((s) => s.query)
  const setQ = useSearch((s) => s.setQuery)
  const mode = useSearch((s) => s.mode)
  const setMode = useSearch((s) => s.setMode)

  const playTrackList = usePlayer((s) => s.playTrackList)
  const currentTrackId = usePlayer((s) => s.current?.id)
  const navigate = useNavigate()

  // Library mode: TanStack Query REST call
  const lib = useLibrarySearch(mode === 'library' ? q : '')

  // Everywhere mode: SSE stream via reducer (transport stays entirely in useEverywhere)
  const everywhere = useEverywhere(q, 'track', mode === 'everywhere')

  const libTracks = lib.data?.tracks ?? []
  const libAlbums = lib.data?.albums ?? []
  const libArtists = lib.data?.artists ?? []

  // ── Empty-query prompt ──────────────────────────────────────────────────────
  if (q.trim() === '') {
    return (
      <div className="space-y-6">
        {/* Mobile-only input — the TopBar has no search bar on mobile. */}
        <MobileSearchInput q={q} onChange={setQ} mode={mode} />

        {/* Scope toggle is always available so you can pick where to search. */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="text-xl font-extrabold text-text-primary">Search</h1>
          <Segmented options={MODE_OPTIONS} value={mode} onChange={setMode} />
        </div>

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
      <MobileSearchInput q={q} onChange={setQ} mode={mode} />

      {/* Results header — title + scope toggle, intentional and aligned. */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="truncate text-xl font-extrabold text-text-primary">
          Results for &ldquo;{q}&rdquo;
        </h1>
        <Segmented options={MODE_OPTIONS} value={mode} onChange={setMode} />
      </div>

      {/* ── Library mode ──────────────────────────────────────────────────── */}
      {mode === 'library' && (
        <>
          {lib.isFetching && <TrackSkeletons />}

          {!lib.isFetching && libTracks.length === 0 && libAlbums.length === 0 && libArtists.length === 0 && lib.isFetched && (
            <EmptyState
              icon="search"
              title="No results"
              hint={`Nothing in your library matches "${q}". Try Everywhere to discover it.`}
            />
          )}

          {libTracks.length > 0 && (
            <section aria-label="Songs">
              <SectionHeading>Songs</SectionHeading>
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
              </div>
            </section>
          )}

          {libAlbums.length > 0 && (
            <section aria-label="Albums">
              <SectionHeading>Albums</SectionHeading>
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
              </div>
            </section>
          )}

          {libArtists.length > 0 && (
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
              </div>
            </section>
          )}
        </>
      )}

      {/* ── Everywhere mode ────────────────────────────────────────────────── */}
      {mode === 'everywhere' && (
        <>
          {/* Source chips */}
          <SourceChipsRow sources={everywhere.sources} />

          {/* Streaming hint — shows while at least one envelope is in flight */}
          {everywhere.status === 'streaming' && (
            <p className="text-xs text-text-muted" aria-live="polite">
              Searching sources...
            </p>
          )}

          {/* Songs */}
          <section aria-label="Songs">
            <SectionHeading>Songs</SectionHeading>
            {everywhere.tracks.length === 0 && everywhere.status === 'streaming' ? (
              <TrackSkeletons />
            ) : everywhere.tracks.length === 0 ? (
              <p className="text-sm text-text-muted">No tracks found.</p>
            ) : (
              <div className="space-y-0.5">
                {everywhere.tracks.map((r) => {
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
                          postDownload(reqFromResult(r))
                            .then((j) => useDownloads.getState().upsert(j))
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

          {/* Albums */}
          {everywhere.albums.length > 0 && (
            <section aria-label="Albums">
              <SectionHeading>Albums</SectionHeading>
              {/* TODO(phase-6): partial N-of-M needs external album tracks + matching */}
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
                {everywhere.albums.map((a) => (
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
                      ) : (
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
                      )
                    }
                  />
                ))}
              </div>
            </section>
          )}

          {/* Artists */}
          {everywhere.artists.length > 0 && (
            <section aria-label="Artists">
              <SectionHeading>Artists</SectionHeading>
              <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
                {everywhere.artists.map((r) => (
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
        </>
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
  mode: Mode
}

function MobileSearchInput({ q, onChange, mode }: MobileSearchInputProps) {
  const placeholder = mode === 'everywhere' ? 'Search everywhere' : 'Search your library'
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
