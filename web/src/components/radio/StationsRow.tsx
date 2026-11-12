import { Carousel } from '../ui/Carousel'
import { MediaCard } from '../ui/MediaCard'
import { Icon } from '../ui/Icon'
import { useStations } from '../../lib/engineApi'
import { useStartRadio } from './useStartRadio'

/**
 * "Radio for you" on Home: stations built from the user's favourite artists and
 * the genres they play. Hidden until the engine has something to suggest.
 */
export function StationsRow() {
  const { data: stations } = useStations(10)
  const { start } = useStartRadio()
  if (!stations || stations.length === 0) return null
  return (
    <div className="mb-8" data-testid="stations-row">
      <Carousel title="Radio for you">
        {stations.map((s) => (
          <MediaCard
            key={`${s.seed.kind}:${s.seed.id}`}
            title={s.label}
            subtitle={s.sublabel}
            coverId={s.coverArtId}
            rounded={s.seed.kind === 'artist' ? 'full' : 'md'}
            badge={<span className="inline-grid place-items-center w-6 h-6 rounded-full bg-black/60 text-white"><Icon name="radio" className="w-3.5 h-3.5" /></span>}
            onClick={() => void start(s.seed)}
            onPlay={() => void start(s.seed)}
          />
        ))}
      </Carousel>
    </div>
  )
}
