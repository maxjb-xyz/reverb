import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { EngineReason } from './engineApi'
import type { ExternalResult, Track } from './types'

/** What happens to songs Reverb downloaded for Discover when the week ends. */
export type KeepRule = 'playlist' | 'played' | 'always'

export interface DiscoverPrefs {
  enabled: boolean
  size: number
  /** Percent of songs that aren't in the library yet. */
  newShare: number
  keepRule: KeepRule
}

export type DiscoverItemState = 'suggested' | 'downloading' | 'kept' | 'removed' | 'expired'

export interface DiscoverItem {
  id: string
  state: DiscoverItemState
  owned: boolean
  track?: Track
  external?: ExternalResult
  reason: EngineReason
  jobId?: string
  jobStatus?: string
  progress?: number
}

export interface DiscoverWeek {
  week: string
  startsAt: number
  endsAt: number
  prefs: DiscoverPrefs
  /** New songs are downloaded ahead of time for this user. */
  downloads: boolean
  items: DiscoverItem[]
}

export interface DiscoverSettings {
  downloads: boolean
  weeklyCap: number
}

export const KEEP_RULES: { value: KeepRule; label: string; hint: string }[] = [
  { value: 'playlist', label: 'Only if I add it to a playlist', hint: 'Songs you didn’t add to a playlist are deleted when the week ends.' },
  { value: 'played', label: 'If I add it to a playlist or play it through', hint: 'Songs you skipped or never played are deleted when the week ends.' },
  { value: 'always', label: 'Always keep', hint: 'Every song Discover downloads stays in your library.' },
]

export const getDiscover = () => api.get<DiscoverWeek>('/discover')
export const getDiscoverPrefs = () => api.get<DiscoverPrefs>('/discover/prefs')
export const putDiscoverPrefs = (p: DiscoverPrefs) => api.put<DiscoverPrefs>('/discover/prefs', p)
export const getDiscoverSettings = () => api.get<DiscoverSettings>('/discover/settings')
export const putDiscoverSettings = (s: DiscoverSettings) => api.put<DiscoverSettings>('/discover/settings', s)

/** True while any song of the week is still downloading. */
export function hasDownloading(w: DiscoverWeek | undefined): boolean {
  return !!w?.items.some((i) => i.state === 'downloading' && !i.owned && i.jobStatus !== 'failed')
}

export function useDiscover() {
  return useQuery({
    queryKey: ['discover'],
    queryFn: getDiscover,
    staleTime: 60_000,
    // Follow the pre-downloads until they've all landed.
    refetchInterval: (q) => (hasDownloading(q.state.data) ? 5_000 : false),
  })
}

export const regenerateDiscover = () => api.post<DiscoverWeek>('/discover/regenerate')

/** Start this week's list over (the keep rule applies to the old one). */
export function useRegenerateDiscover() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: regenerateDiscover, onSuccess: (w) => qc.setQueryData(['discover'], w) })
}

/** Songs Reverb downloaded for this week that the keep rule could delete. */
export function downloadedThisWeek(w: DiscoverWeek): number {
  return w.items.filter((i) => i.jobId && i.state === 'downloading').length
}

export function useDiscoverPrefs() {
  return useQuery({ queryKey: ['discover', 'prefs'], queryFn: getDiscoverPrefs })
}

export function useDiscoverSettings(enabled: boolean) {
  return useQuery({ queryKey: ['discover', 'settings'], queryFn: getDiscoverSettings, enabled })
}

export function useSaveDiscoverPrefs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: putDiscoverPrefs,
    onSuccess: (p) => {
      qc.setQueryData(['discover', 'prefs'], p)
      void qc.invalidateQueries({ queryKey: ['discover'], exact: true })
    },
  })
}

export function useSaveDiscoverSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: putDiscoverSettings,
    onSuccess: (s) => qc.setQueryData(['discover', 'settings'], s),
  })
}

/** The library songs of the week, in order (what Play plays). */
export function discoverTracks(w: DiscoverWeek): Track[] {
  return w.items.flatMap((i) => (i.owned && i.track ? [i.track] : []))
}

/** "Refreshes Monday" style label for the end of the week. */
export function refreshLabel(endsAt: number): string {
  if (!endsAt) return 'New songs every Monday'
  const d = new Date(endsAt * 1000)
  return `New songs ${d.toLocaleDateString(undefined, { weekday: 'long' })}`
}
