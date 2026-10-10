import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Items, ProviderSummary } from './types'

// Display names can change; IDs remain the keys in routes, filters and requests.
// A saved name keeps historical runs and results readable after source removal.
export function sourceName(names: ReadonlyMap<string, string>, id?: string, savedName?: string) {
  return (id && names.get(id)?.trim()) || savedName?.trim() || id || 'Unknown source'
}

export function useSourceNames() {
  const sources = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  return useMemo(() => {
    const names = new Map(sources.data?.items?.map((source) => [source.id, source.name]))
    return (id?: string, savedName?: string) => sourceName(names, id, savedName)
  }, [sources.data])
}
