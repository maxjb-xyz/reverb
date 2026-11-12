import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Carousel } from '../ui/Carousel'
import { MediaCard } from '../ui/MediaCard'
import { MixEditor } from './MixEditor'
import { DiscoverArt } from './DiscoverArt'
import { mixTracks, useMixes } from '../../lib/mixesApi'
import { usePlayer } from '../../lib/playerStore'

/**
 * "Your mixes" on Home: Discover Weekly, the daily mixes Reverb builds from
 * the user's taste, then their own mixes, then a tile to make a new one.
 */
export function MixesRow() {
  const navigate = useNavigate()
  const { data: mixes, isLoading } = useMixes()
  const playTrackList = usePlayer((s) => s.playTrackList)
  const [creating, setCreating] = useState(false)
  if (isLoading) return null
  return (
    <div className="mb-8" data-testid="mixes-row">
      <Carousel title="Your mixes">
        <MediaCard title="Discover Weekly" subtitle="New music for you, every Monday" art={<DiscoverArt />} onClick={() => navigate('/discover')} />
        {(mixes ?? []).map((m) => {
          const tracks = mixTracks(m)
          return (
            <MediaCard
              key={m.id}
              title={m.name}
              subtitle={m.description || undefined}
              coverId={m.coverArtId || undefined}
              onClick={() => navigate(`/mix/${m.id}`)}
              onPlay={tracks.length ? () => playTrackList(tracks, 0, { kind: 'mix', id: m.id }) : undefined}
            />
          )
        })}
        <MediaCard title="New mix" subtitle="Pick artists, genres or a style" ghost onClick={() => setCreating(true)} />
      </Carousel>
      {creating && <MixEditor onClose={() => setCreating(false)} onSaved={(m) => navigate(`/mix/${m.id}`)} />}
    </div>
  )
}
