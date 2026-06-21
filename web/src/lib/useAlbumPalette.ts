import { useEffect, useState } from 'react'
import { getPalette } from './paletteService'
import { contrastTextColor, type RGB } from './palette'
import { useSettings } from './settingsApi'

export interface AlbumPalette {
  rgb: RGB
  text: string
  scrim: boolean
}

// Resolved pairs a palette with the cover URL it was extracted from, so the hook can
// tell whether the stored palette still matches the CURRENTLY requested cover.
interface Resolved {
  url: string
  palette: AlbumPalette
}

// useAlbumPalette returns the dominant-color palette for a cover URL, gated on the
// dynamic_background setting. Returns null while settings load, when the setting is
// off, when there is no cover, or before extraction for the CURRENT cover resolves —
// so a track change never briefly shows the previous track's color.
export function useAlbumPalette(coverUrl: string | undefined): AlbumPalette | null {
  const settings = useSettings()
  const enabled = settings.data?.dynamicBackground === true
  const [resolved, setResolved] = useState<Resolved | null>(null)

  useEffect(() => {
    if (!enabled || !coverUrl) return
    let active = true
    getPalette(coverUrl)
      .then((rgb) => {
        if (active) setResolved({ url: coverUrl, palette: { rgb, ...contrastTextColor(rgb) } })
      })
      .catch(() => {
        if (active) setResolved(null)
      })
    return () => {
      active = false
    }
  }, [enabled, coverUrl])

  // Derive the result instead of writing null into state from the effect (avoids an
  // extra render + the react-hooks/set-state-in-effect lint). Return the stored palette
  // ONLY when it was resolved for the cover we're currently asked about: when the
  // setting is off, there is no cover, or the cover changed and its palette has not yet
  // resolved, return null — never the stale previous color.
  if (!enabled || !coverUrl || resolved?.url !== coverUrl) return null
  return resolved.palette
}
