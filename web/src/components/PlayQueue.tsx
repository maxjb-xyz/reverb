import { useRef } from 'react'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'

export function PlayQueue() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const queue = usePlayer((s) => s.queue)
  const index = usePlayer((s) => s.index)
  const current = usePlayer((s) => s.current)
  const removeAt = usePlayer((s) => s.removeAt)
  const moveItem = usePlayer((s) => s.moveItem)
  const jumpTo = usePlayer((s) => s.jumpTo)

  const dragFrom = useRef<number | null>(null)

  if (rightPanel !== 'queue') return null

  const upNext = queue
    .map((t, i) => ({ t, i }))
    .filter(({ i }) => i !== index)

  return (
    <aside className="absolute right-0 top-0 z-20 flex h-full w-80 flex-col border-l border-neutral-800 bg-neutral-950/95 backdrop-blur">
      <div className="flex items-center justify-between border-b border-neutral-800 p-4">
        <h2 className="text-lg font-bold">Play Queue</h2>
        <button type="button" aria-label="Close queue" onClick={closePanel} className="text-neutral-400 hover:text-white">
          ✕
        </button>
      </div>

      <div className="border-b border-neutral-800 p-4">
        <div className="mb-2 text-xs uppercase tracking-wide text-neutral-500">Now Playing</div>
        {current ? (
          <div className="flex items-center gap-3">
            {current.coverArtId ? (
              <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-10 w-10 rounded object-cover" />
            ) : (
              <div className="h-10 w-10 rounded bg-neutral-800" />
            )}
            <div className="min-w-0">
              <div className="truncate text-sm font-medium text-accent">{current.title}</div>
              <div className="truncate text-xs text-neutral-400">{current.artist}</div>
            </div>
          </div>
        ) : (
          <div className="text-sm text-neutral-500">Nothing playing</div>
        )}
      </div>

      <div className="flex-1 overflow-auto p-2">
        <div className="px-2 py-1 text-xs uppercase tracking-wide text-neutral-500">Up Next</div>
        <ul>
          {upNext.map(({ t, i }) => (
            <li
              key={`${t.id}-${i}`}
              draggable
              onClick={() => jumpTo(i)}
              onDragStart={() => (dragFrom.current = i)}
              onDragOver={(e) => e.preventDefault()}
              onDrop={() => {
                if (dragFrom.current !== null && dragFrom.current !== i) {
                  moveItem(dragFrom.current, i)
                }
                dragFrom.current = null
              }}
              className="flex items-center gap-2 rounded px-2 py-1.5 hover:bg-neutral-800 cursor-pointer"
            >
              <span className="cursor-grab text-neutral-600">⠿</span>
              <div className="min-w-0 flex-1">
                <div className="truncate text-sm">{t.title}</div>
                <div className="truncate text-xs text-neutral-400">{t.artist}</div>
              </div>
              <button
                type="button"
                aria-label={`Remove ${t.title}`}
                onClick={(e) => { e.stopPropagation(); removeAt(i) }}
                className="text-neutral-500 hover:text-accent"
              >
                ✕
              </button>
            </li>
          ))}
          {upNext.length === 0 && <li className="px-2 py-4 text-sm text-neutral-500">Queue is empty.</li>}
        </ul>
      </div>
    </aside>
  )
}
