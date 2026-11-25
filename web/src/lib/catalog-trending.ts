import {
  catalogCardContext,
  catalogCardSummary,
  catalogCategories,
  catalogPrimaryYear,
  catalogSubtypeLabel,
  catalogTorrentCount,
  type CatalogCardKind,
  type CatalogCategoryID,
} from './catalog-cards.ts'

export type TrendingPeriod = 'day' | 'week' | 'month' | 'quarter' | 'year'

export type TrendingCategoryID = CatalogCategoryID

export const defaultTrendingPeriod: TrendingPeriod = 'week'

export const trendingPeriods: readonly { value: TrendingPeriod; label: string }[] = [
  { value: 'day', label: 'Past day' },
  { value: 'week', label: 'Past week' },
  { value: 'month', label: 'Past month' },
  { value: 'quarter', label: 'Past quarter' },
  { value: 'year', label: 'Past year' },
]

export const trendingCategories = catalogCategories

export type CatalogTrendingItem = {
  id: number
  type: CatalogCardKind
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

export const trendingPrimaryYear = catalogPrimaryYear
export const trendingItemContext = catalogCardContext
export const trendingItemSubtype = catalogSubtypeLabel
export const trendingItemSummary = catalogCardSummary
export const trendingTorrentCount = catalogTorrentCount

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
  return `${trendingItemSummary(item)} · ${trendingMetricLabel(item)}`
}
