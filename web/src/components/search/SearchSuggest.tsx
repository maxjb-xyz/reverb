import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Cover, Icon, Skeleton } from '../ui'
import { useLibrarySearch, coverUrl } from '../../lib/libraryApi'
import { usePlayer } from '../../lib/playerStore'
import type { Track } from '../../lib/types'

interface SearchSuggestProps {
  query: string
  onNavigateAll: () => void
  onClose: () => void
}

// Small debounce so we don't fire a REST query on every keystroke.
function useDebounced<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(id)
  }, [value, delayMs])
  return debounced
}

const MAX_TRACKS = 4
const MAX_ALBUMS = 3
const MAX_ARTISTS = 3

/**
 * SearchSuggest — Spotify-style typeahead preview shown under the TopBar search
 * input. Queries the fast library REST endpoint (the full Everywhere SSE stays
 * on the /search page) and renders a small preview of matching tracks, albums
 * and artists. Mirrors the Escape-to-close pattern of DownloadPopover; the
 * TopBar owns the outside-click + open/close state.
 */
export function SearchSuggest({ query, onNavigateAll, onClose }: SearchSuggestProps) {
  const navigate = useNavigate()
  const debouncedQuery = useDebounced(query, 200)
  const lib = useLibrarySearch(debouncedQuery)
  const panelRef = useRef<HTMLDivElement>(null)

  // Esc closes (outside-click is handled by the TopBar, which owns the anchor).
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  const tracks = (lib.data?.tracks ?? []).slice(0, MAX_TRACKS)
  const albums = (lib.data?.albums ?? []).slice(0, MAX_ALBUMS)
  const artists = (lib.data?.artists ?? []).slice(0, MAX_ARTISTS)

  // Loading on the *current* query (not a stale render of the previous one).
  const loading = lib.isFetching && debouncedQuery.trim() !== ''
  const hasResults = tracks.length > 0 || albums.length > 0 || artists.length > 0

  function playTrack(t: Track) {
    usePlayer.getState().playTrackList([t], 0)
    onClose()
  }

  return (
    <div
      ref={panelRef}
      role="listbox"
      aria-label="Search suggestions"
      className={[
        'absolute left-0 right-0 top-full z-50 mt-2 w-full',
        'max-h-[70vh] overflow-y-auto',
        'rounded-xl border border-border-subtle bg-raised shadow-pop',
        'p-1.5',
      ].join(' ')}
    >
      {loading && !hasResults ? (
        <div className="space-y-1 p-1" aria-busy="true" aria-label="Loading suggestions">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-11 w-full" />
          ))}
        </div>
      ) : !hasResults ? (
        <p className="px-3 py-3 text-sm text-text-muted">
          No matches in your library — press Enter to search everywhere.
        </p>
      ) : (
        <>
          {tracks.length > 0 && (
            <SuggestSection label="Songs">
              {tracks.map((t) => (
                <SuggestRow
                  key={t.id}
                  coverId={t.coverArtId}
                  rounded="md"
                  title={t.title}
                  subtitle={t.artist}
                  accessibleName={`Play ${t.title} by ${t.artist}`}
                  onClick={() => playTrack(t)}
                />
              ))}
            </SuggestSection>
          )}

          {albums.length > 0 && (
            <SuggestSection label="Albums">
              {albums.map((al) => (
                <SuggestRow
                  key={al.id}
                  coverId={al.coverArtId}
                  rounded="md"
                  title={al.name}
                  subtitle={al.artist}
                  accessibleName={`Open album ${al.name}`}
                  onClick={() => {
                    navigate(`/album/${al.id}`)
                    onClose()
                  }}
                />
              ))}
            </SuggestSection>
          )}

          {artists.length > 0 && (
            <SuggestSection label="Artists">
              {artists.map((ar) => (
                <SuggestRow
                  key={ar.id}
                  coverId={ar.coverArtId}
                  rounded="full"
                  title={ar.name}
                  subtitle="Artist"
                  accessibleName={`Open artist ${ar.name}`}
                  onClick={() => {
                    navigate(`/artist/${ar.id}`)
                    onClose()
                  }}
                />
              ))}
            </SuggestSection>
          )}
        </>
      )}

      {/* Footer — always available so Enter / click both reach the full page. */}
      <div className="border-t border-border-subtle p-1 pt-1.5">
        <button
          type="button"
          onClick={onNavigateAll}
          className={[
            'flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors',
            'hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <span className="flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-surface text-accent">
            <Icon name="search" className="text-base" />
          </span>
          <span className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary">
            See all results for &ldquo;{query}&rdquo;
          </span>
        </button>
      </div>
    </div>
  )
}

// ── Section + row primitives ──────────────────────────────────────────────────

function SuggestSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="pb-1">
      <p className="px-2.5 pb-1 pt-2 text-xs font-bold uppercase tracking-wider text-text-muted">
        {label}
      </p>
      <div role="group" aria-label={label}>
        {children}
      </div>
    </div>
  )
}

interface SuggestRowProps {
  coverId?: string
  rounded: 'md' | 'full'
  title: string
  subtitle: string
  accessibleName: string
  onClick: () => void
}

function SuggestRow({ coverId, rounded, title, subtitle, accessibleName, onClick }: SuggestRowProps) {
  const src = coverId ? coverUrl(coverId, 80) : undefined
  return (
    <button
      type="button"
      role="option"
      aria-selected={false}
      aria-label={accessibleName}
      onClick={onClick}
      className={[
        'flex w-full items-center gap-3 rounded-lg px-2.5 py-1.5 text-left transition-colors',
        'hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80',
      ].join(' ')}
    >
      <Cover src={src} alt={title} size={40} rounded={rounded} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-semibold text-text-primary">{title}</span>
        <span className="block truncate text-xs text-text-muted">{subtitle}</span>
      </span>
    </button>
  )
}
