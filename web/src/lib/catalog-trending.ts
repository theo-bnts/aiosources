export type TrendingPeriod = 'day' | 'week' | 'month' | 'quarter' | 'year'

export type TrendingCategoryID = 'videos' | 'games' | 'software' | 'music' | 'books'

export const defaultTrendingPeriod: TrendingPeriod = 'week'

export const trendingPeriods: readonly { value: TrendingPeriod; label: string }[] = [
  { value: 'day', label: 'Past day' },
  { value: 'week', label: 'Past week' },
  { value: 'month', label: 'Past month' },
  { value: 'quarter', label: 'Past quarter' },
  { value: 'year', label: 'Past year' },
]

export const trendingCategories: readonly {
  id: TrendingCategoryID
  label: string
  frame: 'portrait' | 'square' | 'icon'
}[] = [
  { id: 'videos', label: 'Videos', frame: 'portrait' },
  { id: 'games', label: 'Games', frame: 'portrait' },
  { id: 'software', label: 'Software', frame: 'icon' },
  { id: 'music', label: 'Music', frame: 'square' },
  { id: 'books', label: 'Books', frame: 'portrait' },
]

export type CatalogTrendingItem = {
  id: number
  type: 'movie' | 'tv' | 'game' | 'software' | 'music' | 'book'
  title: string
  years: number[]
  cover?: string
  torrents: number
  episodes?: number
  seasons?: number
  torrents_per_episode?: number
  torrents_per_season?: number
  artists?: string[]
  authors?: string[]
  vendor?: string
  music_type?: string
}

export type CatalogTrendingCategory = {
  id: TrendingCategoryID
  items: CatalogTrendingItem[]
}

export type CatalogTrending = {
  period: TrendingPeriod
  categories: CatalogTrendingCategory[]
}

export function parseTrendingPeriod(value: string | null): TrendingPeriod {
  if (value && trendingPeriods.some((option) => option.value === value)) {
    return value as TrendingPeriod
  }
  return defaultTrendingPeriod
}

export function trendingPeriodLabel(period: TrendingPeriod): string {
  return trendingPeriods.find((option) => option.value === period)?.label ?? 'Past week'
}

export function trendingPrimaryYear(years?: number[]): number | undefined {
  if (!years?.length) return undefined
  const valid = years.filter((year) => Number.isInteger(year) && year > 0)
  return valid.length ? Math.min(...valid) : undefined
}

export function trendingItemContext(item: CatalogTrendingItem): string | undefined {
  const year = trendingPrimaryYear(item.years)
  if (year) return String(year)
  if (item.vendor) return item.vendor
  if (item.artists?.length) return item.artists[0]
  if (item.authors?.length) return item.authors[0]
  return undefined
}

export function trendingTorrentCount(torrents: number): string {
  return `${torrents.toLocaleString()} ${torrents === 1 ? 'torrent' : 'torrents'}`
}

function formatTrendingAverage(value: number): string {
  if (value >= 100) return Math.round(value).toLocaleString()
  const rounded = Math.round(value * 10) / 10
  return Number.isInteger(rounded) ? rounded.toLocaleString() : rounded.toFixed(1)
}

export function trendingMetricLabel(item: CatalogTrendingItem): string {
  if (item.type === 'tv' && item.torrents_per_episode && item.torrents_per_episode > 0) {
    return `${formatTrendingAverage(item.torrents_per_episode)}/ep`
  }
  if (item.type === 'tv' && item.torrents_per_season && item.torrents_per_season > 0) {
    return `${formatTrendingAverage(item.torrents_per_season)}/season`
  }
  return trendingTorrentCount(item.torrents)
}

export function trendingMetricTitle(item: CatalogTrendingItem): string | undefined {
  if (
    item.type === 'tv' &&
    item.torrents_per_episode &&
    item.torrents_per_episode > 0 &&
    item.episodes &&
    item.episodes > 0
  ) {
    const episodes = `${item.episodes.toLocaleString()} ${item.episodes === 1 ? 'episode' : 'episodes'}`
    return `${trendingTorrentCount(item.torrents)} · ${episodes} (${formatTrendingAverage(item.torrents_per_episode)}/ep)`
  }
  if (
    item.type === 'tv' &&
    item.torrents_per_season &&
    item.torrents_per_season > 0 &&
    item.seasons &&
    item.seasons > 0
  ) {
    const seasons = `${item.seasons.toLocaleString()} ${item.seasons === 1 ? 'season' : 'seasons'}`
    return `${trendingTorrentCount(item.torrents)} · ${seasons} (${formatTrendingAverage(item.torrents_per_season)}/season)`
  }
  return undefined
}

export function formatTrendingMeta(item: CatalogTrendingItem): string {
  const metric = trendingMetricLabel(item)
  const context = trendingItemContext(item)
  return context ? `${context} · ${metric}` : metric
}
