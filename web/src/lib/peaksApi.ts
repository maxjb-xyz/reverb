import { useQuery } from '@tanstack/react-query'

export function usePeaks(trackId: string | undefined) {
  return useQuery({
    queryKey: ['peaks', trackId],
    queryFn: async () => {
      const response = await fetch(`/api/v1/library/track/${encodeURIComponent(trackId!)}/peaks`, { credentials: 'include' })
      if (response.status === 204) return null
      if (!response.ok) throw new Error(`peaks ${response.status}`)
      return (await response.json() as { peaks: number[] }).peaks
    },
    enabled: !!trackId,
    staleTime: Infinity,
    retry: false,
  })
}
