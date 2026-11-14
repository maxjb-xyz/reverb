import { useNavigate } from 'react-router-dom'
import { Carousel } from '../ui/Carousel'
import { MediaCard } from '../ui/MediaCard'
import { useSimilarArtists } from '../../lib/engineApi'

/**
 * "Fans also like" on an artist page, from the engine's artist-similarity map.
 * Artists in the library link to their library page; the rest open their
 * online page (where they can be downloaded or requested) and are drawn as
 * ghost cards.
 */
export function FansAlsoLike({ artist }: { artist: string }) {
  const navigate = useNavigate()
  const { data } = useSimilarArtists(artist)
  const items = (data?.similar ?? []).filter((s) => s.libraryArtistId || s.externalId).slice(0, 12)
  if (items.length === 0) return null
  return (
    <div data-testid="fans-also-like">
      <Carousel title="Fans also like">
        {items.map((s) => {
          const owned = !!s.libraryArtistId
          const to = owned ? `/artist/library/${s.libraryArtistId}` : `/artist/${s.source}/${s.externalId}`
          return (
            <MediaCard
              key={s.key}
              title={s.name}
              subtitle={owned ? 'In your library' : 'Not in your library'}
              coverSrc={s.imageUrl || undefined}
              rounded="full"
              ghost={!owned}
              onClick={() => navigate(to)}
            />
          )
        })}
      </Carousel>
    </div>
  )
}
