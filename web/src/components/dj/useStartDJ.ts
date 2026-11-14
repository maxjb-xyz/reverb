import { usePlayer } from '../../lib/playerStore'
import { useAuthStore } from '../../lib/authStore'
import { useToastStore } from '../../lib/toastStore'
import { askDJ, changeVibe, DJEmptyError, startDJ, useDJ } from '../../lib/dj'

/** Start the DJ, change lanes or ask it for something, reporting failures as toasts. */
export function useStartDJ() {
  const play = usePlayer((s) => s.playTrackList)
  const push = useToastStore((s) => s.push)
  const busy = useDJ((s) => s.busy)

  async function start() {
    if (busy) return
    try {
      await startDJ({ play, listener: () => useAuthStore.getState().me?.username })
    } catch (e) {
      push(
        e instanceof DJEmptyError
          ? 'Not enough in your library for the DJ yet. Reverb is still learning what you play, try again in a minute.'
          : "Couldn't start the DJ.",
        'error',
      )
    }
  }

  async function change() {
    if (busy) return
    if (!(await changeVibe())) push('Nothing else to switch to right now.', 'info')
  }

  /** Ask for something. True when the DJ is playing it. */
  async function ask(text: string): Promise<boolean> {
    if (busy || !text.trim()) return false
    const r = await askDJ(text)
    switch (r.status) {
      case 'playing':
        return true
      case 'not-understood':
        // A missing artist gets its own offer in the bar.
        if (!r.missing) push(notUnderstood(r.try), 'info')
        return false
      case 'error':
        push(r.limited ? 'Give the DJ a moment, then ask again.' : "Couldn't reach the DJ. Try again in a moment.", 'error')
        return false
      case 'empty':
        if (!r.missing) push('Nothing in your library fits that yet.', 'info')
        return false
      default:
        return false
    }
  }

  return { start, change, ask, busy }
}

/** What to say when the DJ couldn't read a request, with library names to try. */
export function notUnderstood(names?: string[]): string {
  const base = "The DJ didn't catch that."
  if (!names?.length) return `${base} Try an artist, a genre, a decade or a mood.`
  return `${base} Try ${names.map((n) => `“${n}”`).join(' or ')}, a decade or a mood.`
}
