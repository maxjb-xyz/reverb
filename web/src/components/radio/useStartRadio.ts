import { useState } from 'react'
import { usePlayer } from '../../lib/playerStore'
import { startRadio, EmptyStationError } from '../../lib/radio'
import { useToastStore } from '../../lib/toastStore'
import type { EngineSeed } from '../../lib/engineApi'

/** Start a station and report failures as a toast. Shared by the buttons. */
export function useStartRadio() {
  const play = usePlayer((s) => s.playTrackList)
  const push = useToastStore((s) => s.push)
  const [busy, setBusy] = useState(false)
  async function start(seed: EngineSeed) {
    if (busy) return
    setBusy(true)
    try {
      const st = await startRadio(seed, play)
      push(`Playing ${st.label}`, 'success')
    } catch (e) {
      push(
        e instanceof EmptyStationError
          ? "Not enough in your library for this station yet. Reverb is still indexing, try again in a minute."
          : "Couldn't start the radio.",
        'error',
      )
    } finally {
      setBusy(false)
    }
  }
  return { start, busy }
}
