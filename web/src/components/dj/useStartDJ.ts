import { usePlayer } from '../../lib/playerStore'
import { useAuthStore } from '../../lib/authStore'
import { useToastStore } from '../../lib/toastStore'
import { changeVibe, DJEmptyError, startDJ, useDJ } from '../../lib/dj'

/** Start the DJ, or change lanes, reporting failures as toasts. */
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

  return { start, change, busy }
}
