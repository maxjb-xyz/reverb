import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface AppSettings {
  accentColor: string
  dynamicBackground: boolean
}

const DEFAULT_ACCENT_CHANNELS = '240 53 75' // #F0354B

// hexToRgbChannels converts "#RRGGBB" (or "RRGGBB") to "r g b" space-separated
// channels for the --color-accent CSS custom property. Falls back to the default
// red channels for any malformed input.
export function hexToRgbChannels(hex: string): string {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(hex.trim())
  if (!m) return DEFAULT_ACCENT_CHANNELS
  const n = parseInt(m[1], 16)
  const r = (n >> 16) & 0xff
  const g = (n >> 8) & 0xff
  const b = n & 0xff
  return `${r} ${g} ${b}`
}

// applyAccent writes the accent color into the --color-accent CSS var live, so the
// whole app (Tailwind `accent` references rgb(var(--color-accent) / a)) re-themes.
export function applyAccent(hex: string): void {
  document.documentElement.style.setProperty('--color-accent', hexToRgbChannels(hex))
}

export function getSettings(): Promise<AppSettings> {
  return api.get<AppSettings>('/settings')
}
export function putSettings(b: Partial<AppSettings>): Promise<AppSettings> {
  return api.put<AppSettings>('/settings', b)
}

export function useSettings() {
  return useQuery({ queryKey: ['settings'], queryFn: getSettings })
}
