import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { EngineItem, EngineSeed, Lane } from './engineApi'

export type MixRefresh = 'daily' | 'weekly' | 'manual'

export interface MixRecipe {
  seeds: EngineSeed[]
  lane?: Lane
  fromYear?: number
  toYear?: number
  size: number
  refresh: MixRefresh
}

export interface Mix {
  id: string
  kind: 'daily' | 'custom'
  name: string
  description: string
  recipe: MixRecipe
  items: EngineItem[]
  coverArtId?: string
  generatedAt: number
  createdAt: number
}

export interface Genre {
  key: string
  label: string
  tracks: number
}

export const listMixes = () => api.get<{ mixes: Mix[] }>('/mixes').then((r) => r.mixes)
export const getMix = (id: string) => api.get<Mix>(`/mixes/${encodeURIComponent(id)}`)
export const createMix = (name: string, recipe: MixRecipe) => api.post<Mix>('/mixes', { name, recipe })
export const updateMix = (id: string, name: string, recipe: MixRecipe) =>
  api.put<Mix>(`/mixes/${encodeURIComponent(id)}`, { name, recipe })
export const refreshMix = (id: string) => api.post<Mix>(`/mixes/${encodeURIComponent(id)}/refresh`)
export const deleteMix = (id: string) => api.del<null>(`/mixes/${encodeURIComponent(id)}`)
export const getGenres = () => api.get<{ genres: Genre[] }>('/engine/genres').then((r) => r.genres)

export function useMixes() {
  return useQuery({ queryKey: ['mixes'], queryFn: listMixes, staleTime: 5 * 60_000 })
}

export function useMix(id: string | undefined) {
  return useQuery({ queryKey: ['mixes', id], queryFn: () => getMix(id as string), enabled: !!id })
}

export function useGenres(enabled = true) {
  return useQuery({ queryKey: ['engine', 'genres'], queryFn: getGenres, enabled, staleTime: 10 * 60_000 })
}

/** Create, update, refresh and delete, keeping the list and the mix page in sync. */
export function useMixMutations() {
  const qc = useQueryClient()
  const put = (m: Mix) => {
    qc.setQueryData(['mixes', m.id], m)
    void qc.invalidateQueries({ queryKey: ['mixes'], exact: true })
  }
  return {
    create: useMutation({ mutationFn: (v: { name: string; recipe: MixRecipe }) => createMix(v.name, v.recipe), onSuccess: put }),
    update: useMutation({ mutationFn: (v: { id: string; name: string; recipe: MixRecipe }) => updateMix(v.id, v.name, v.recipe), onSuccess: put }),
    refresh: useMutation({ mutationFn: refreshMix, onSuccess: put }),
    remove: useMutation({
      mutationFn: deleteMix,
      onSuccess: (_, id) => {
        qc.removeQueries({ queryKey: ['mixes', id] })
        void qc.invalidateQueries({ queryKey: ['mixes'], exact: true })
      },
    }),
  }
}

/** The library tracks of a mix, in order (what Play plays). */
export function mixTracks(m: Mix) {
  return m.items.flatMap((i) => (i.owned && i.track ? [i.track] : []))
}

const LANE_LABELS: Record<string, string> = {
  '': 'Radio',
  'on-repeat': 'On repeat',
  throwback: 'Throwback',
  'deep-cuts': 'Deep cuts',
  'sounds-like': 'Sounds like',
  'new-to-you': 'New to you',
  'fresh-finds': 'Fresh finds',
}

export function laneLabel(lane: Lane | undefined): string {
  return LANE_LABELS[lane ?? ''] ?? 'Radio'
}
