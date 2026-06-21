import { useEffect, useState } from 'react'
import { getPalette } from './paletteService'
import { contrastTextColor, type RGB } from './palette'
import { useSettings } from './settingsApi'

export interface AlbumPalette {
  rgb: RGB
  text: string
  scrim: boolean
}

// useAlbumPalette returns the dominant-color palette for a cover URL, gated on the
// dynamic_background setting. Returns null while settings load, when the setting is
// off, when there is no cover, or before extraction resolves. Stale resolutions are
// dropped when the cover URL changes mid-flight.
export function useAlbumPalette(coverUrl: string | undefined): AlbumPalette | null {
  const settings = useSettings()
  const enabled = settings.data?.dynamicBackground === true
  const [palette, setPalette] = useState<AlbumPalette | null>(null)

  useEffect(() => {
    if (!enabled || !coverUrl) {
      setPalette(null)
      return
    }
    let active = true
    getPalette(coverUrl)
      .then((rgb) => {
        if (!active) return
        setPalette({ rgb, ...contrastTextColor(rgb) })
      })
      .catch(() => {
        if (active) setPalette(null)
      })
    return () => {
      active = false
    }
  }, [enabled, coverUrl])

  return palette
}
