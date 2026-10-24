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

export const torrentSortOptions: { value: TorrentSort; label: string }[] = [
  { value: 'recent', label: 'Most recent' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'size_desc', label: 'Largest first' },
  { value: 'size_asc', label: 'Smallest first' },
  { value: 'name_asc', label: 'Name (A–Z)' },
  { value: 'name_desc', label: 'Name (Z–A)' },
  { value: 'relevance_asc', label: 'Search relevance (ascending)' },
  { value: 'relevance', label: 'Search relevance' },
]

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

export function occurrenceHref(id: string) {
  return `/torrents?${new URLSearchParams({ occurrence: id })}`
}

export const torrentSorting = [
  { label: 'Last imported', asc: 'oldest', desc: 'recent' },
  { label: 'Size', asc: 'size_asc', desc: 'size_desc' },
  { label: 'Name', asc: 'name_asc', desc: 'name_desc' },
  { label: 'Search relevance', asc: 'relevance_asc', desc: 'relevance' },
]
