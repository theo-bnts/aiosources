export type CatalogCategoryID = 'videos' | 'games' | 'software' | 'music' | 'books'

export type CatalogCardKind = 'movie' | 'tv' | 'game' | 'software' | 'music' | 'book'

export type CatalogCardFrame = 'portrait' | 'square' | 'icon'

export type CatalogCategorySpec = {
  id: CatalogCategoryID
  label: string
  frame: CatalogCardFrame
}

export const catalogCategories: readonly CatalogCategorySpec[] = [
  { id: 'videos', label: 'Videos', frame: 'portrait' },
  { id: 'games', label: 'Games', frame: 'portrait' },
  { id: 'software', label: 'Software', frame: 'icon' },
  { id: 'music', label: 'Music', frame: 'square' },
  { id: 'books', label: 'Books', frame: 'portrait' },
]

export function catalogCategoryByGroup(
  group: string,
): CatalogCategorySpec | { id: 'catalog'; label: string; frame: CatalogCardFrame } {
  const matched = catalogCategories.find((category) => category.label === group)
  return matched ?? { id: 'catalog', label: group || 'Catalog', frame: 'portrait' }
}

export function catalogSubtypeLabel(item: { type: CatalogCardKind; music_type?: string }): string {
  switch (item.type) {
    case 'movie':
      return 'Movie'
    case 'tv':
      return 'Series'
    case 'game':
      return 'Game'
    case 'software':
      return 'Software'
    case 'music':
      return item.music_type || 'Music'
    case 'book':
      return 'Book'
  }
}

export function catalogPrimaryYear(years?: number[]): number | undefined {
  if (!years?.length) return undefined
  const valid = years.filter((year) => Number.isInteger(year) && year > 0)
  return valid.length ? Math.min(...valid) : undefined
}

export function catalogCardContext(item: {
  years?: number[]
  vendor?: string
  artists?: string[]
  authors?: string[]
}): string | undefined {
  const year = catalogPrimaryYear(item.years)
  if (year) return String(year)
  if (item.vendor) return item.vendor
  if (item.artists?.length) return item.artists[0]
  if (item.authors?.length) return item.authors[0]
  return undefined
}

export function catalogCardSummary(item: {
  type: CatalogCardKind
  music_type?: string
  years?: number[]
  vendor?: string
  artists?: string[]
  authors?: string[]
}): string {
  const subtype = catalogSubtypeLabel(item)
  const context = catalogCardContext(item)
  return context ? `${subtype} · ${context}` : subtype
}

export function catalogTorrentCount(torrents: number): string {
  return `${torrents.toLocaleString()} ${torrents === 1 ? 'torrent' : 'torrents'}`
}
