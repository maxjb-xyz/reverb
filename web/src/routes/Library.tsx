import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAlbums, useArtists, usePlaylists } from '../lib/libraryApi'
import { Chip, MediaCard, Skeleton, EmptyState } from '../components/ui'

type Filter = 'albums' | 'artists' | 'playlists'

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
  { key: 'playlists', label: 'Playlists' },
]

const SKELETON_COUNT = 10

function SkeletonGrid({ rounded = 'md' }: { rounded?: 'md' | 'full' }) {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
      {Array.from({ length: SKELETON_COUNT }).map((_, i) => (
        <div key={i} className="space-y-2 p-3 rounded-lg bg-raised">
          <Skeleton
            className="aspect-square w-full"
            rounded={rounded}
            data-testid="skeleton-cover"
          />
          <Skeleton className="h-3 w-3/4" />
          <Skeleton className="h-2 w-1/2" />
        </div>
      ))}
    </div>
  )
}

export default function Library() {
  const [filter, setFilter] = useState<Filter>('albums')
  const navigate = useNavigate()

  const albums = useAlbums('newest')
  const artists = useArtists()
  const playlists = usePlaylists()

  return (
    <div className="space-y-6">
      {/* Page header */}
      <h1 className="text-2xl font-bold text-text-primary">Your Library</h1>

      {/* Filter chips */}
      <div className="flex gap-2 flex-wrap" role="group" aria-label="Library filter">
        {FILTERS.map(({ key, label }) => (
          <Chip
            key={key}
            selected={filter === key}
            onClick={() => setFilter(key)}
          >
            {label}
          </Chip>
        ))}
      </div>

      {/* Albums grid */}
      {filter === 'albums' && (
        <>
          {albums.isLoading ? (
            <SkeletonGrid rounded="md" />
          ) : (albums.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Download some music to start building your library."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(albums.data ?? []).map((al) => (
                <MediaCard
                  key={al.id}
                  title={al.name}
                  subtitle={al.artist}
                  coverId={al.coverArtId || undefined}
                  rounded="md"
                  onClick={() => navigate(`/album/${al.id}`)}
                />
              ))}
            </div>
          )}
        </>
      )}

      {/* Artists grid */}
      {filter === 'artists' && (
        <>
          {artists.isLoading ? (
            <SkeletonGrid rounded="full" />
          ) : (artists.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Download some music to start building your library."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(artists.data ?? []).map((ar) => (
                <MediaCard
                  key={ar.id}
                  title={ar.name}
                  coverId={ar.coverArtId || undefined}
                  rounded="full"
                  onClick={() => navigate(`/artist/${ar.id}`)}
                />
              ))}
            </div>
          )}
        </>
      )}

      {/* Playlists grid */}
      {filter === 'playlists' && (
        <>
          {playlists.isLoading ? (
            <SkeletonGrid rounded="md" />
          ) : (playlists.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Create a playlist or download some music to get started."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(playlists.data ?? []).map((pl) => (
                <MediaCard
                  key={pl.id}
                  title={pl.name}
                  subtitle={`${pl.songCount} track${pl.songCount !== 1 ? 's' : ''}`}
                  coverId={pl.coverArtId || undefined}
                  rounded="md"
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
