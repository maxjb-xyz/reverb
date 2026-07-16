import { useEffect } from 'react'
import { usePlayer } from './playerStore'

/** Keeps browser tabs useful while browsing, and surfaces the current track. */
export function useDocumentTitle(page?: string) {
  const current = usePlayer((s) => s.current)

  useEffect(() => {
    document.title = current
      ? `${current.title} – ${current.artist} · Reverb`
      : page
        ? `${page} · Reverb`
        : 'Reverb'
  }, [current, page])
}
