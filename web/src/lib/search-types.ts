export type TorrentSort =
  | 'recent'
  | 'oldest'
  | 'size_desc'
  | 'size_asc'
  | 'name_asc'
  | 'name_desc'
  | 'relevance'
  | 'relevance_asc'

export interface TorrentFilters {
  q?: string
  providers?: string[]
  category?: string
  min_size?: string
  max_size?: string
  published_after?: string
  published_before?: string
  info_hash?: string
  sort?: TorrentSort
}

export const torrentFilterKeys = [
  'q',
  'category',
  'min_size',
  'max_size',
  'published_after',
  'published_before',
  'info_hash',
  'sort',
] as const

export function readTorrentFilters(search: URLSearchParams): TorrentFilters {
  const filters: TorrentFilters = {}
  for (const key of torrentFilterKeys) {
    const value = search.get(key)
    if (key === 'sort' && value === 'seeders_desc') continue
    if (value) Object.assign(filters, { [key]: value })
  }
  const providers = search.getAll('provider').filter(Boolean)
  if (providers.length) filters.providers = providers
  return filters
}

export function torrentFilterParams(filters: TorrentFilters) {
  const search = new URLSearchParams()
  for (const key of torrentFilterKeys) {
    if (filters[key]) search.set(key, filters[key])
  }
  for (const provider of filters.providers ?? []) search.append('provider', provider)
  return search
}

export const torrentSorting = [
  { label: 'Last imported', asc: 'oldest', desc: 'recent' },
  { label: 'Size', asc: 'size_asc', desc: 'size_desc' },
  { label: 'Name', asc: 'name_asc', desc: 'name_desc' },
  { label: 'Search relevance', asc: 'relevance_asc', desc: 'relevance' },
]
