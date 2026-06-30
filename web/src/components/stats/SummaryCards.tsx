import type { SummaryStats } from '../../lib/statsApi'
import { msToHuman } from '../../lib/range'

interface Props {
  data: SummaryStats
}

interface CardProps {
  label: string
  value: string | number
}

function StatCard({ label, value }: CardProps) {
  return (
    <div className="flex flex-col gap-1 px-5 py-4 rounded-lg bg-surface-raised">
      <span className="text-xs font-semibold uppercase tracking-wider text-secondary">
        {label}
      </span>
      <span className="text-3xl font-black tabular-nums text-primary leading-none">
        {value}
      </span>
    </div>
  )
}

export function SummaryCards({ data }: Props) {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
      <StatCard label="Songs played" value={data.Plays} />
      <StatCard label="Time listened" value={msToHuman(data.MsPlayed)} />
      <StatCard label="Tracks" value={data.DistinctTracks} />
      <StatCard label="Artists" value={data.DistinctArtists} />
      <StatCard label="Albums" value={data.DistinctAlbums} />
    </div>
  )
}
