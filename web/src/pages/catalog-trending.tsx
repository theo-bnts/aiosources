import { keepPreviousData, useQuery } from '@tanstack/react-query'

import { params } from '@/lib/api'
import {
  defaultTrendingPeriod,
  parseTrendingPeriod,
  trendingCategories,
  trendingItemSummary,
  trendingMetricLabel,
  trendingMetricTitle,
  trendingPeriodLabel,
  trendingPeriods,
  type CatalogTrending,
  type CatalogTrendingItem,
  type TrendingCategoryID,
  type TrendingPeriod,
} from '@/lib/catalog-trending'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import { browserLanguage } from '@/lib/language-preferences'
import {
  catalogAPI,
  catalogQueryKey,
  catalogSearchPath,
  usePublicSearch,
} from '@/lib/public-search'
import { cn } from '@/lib/utils'
import { CatalogPosterRail, type CatalogPosterCardItem } from '@/components/catalog-poster-rail'
import { ErrorNotice } from '@/components/common'
import { Label } from '@/components/ui/label'

export function CatalogTrendingPage() {
  const visitor = usePublicSearch()
  const searchPath = catalogSearchPath(visitor)
  const language = browserLanguage()
  const [search, setSearch] = useViewSearchParams()
  const listState = useListViewState()
  const period = parseTrendingPeriod(search.get('period'))
  const periodLabel = trendingPeriodLabel(period)

  const query = useQuery({
    queryKey: catalogQueryKey(visitor, 'trending', period, language),
    queryFn: ({ signal }) =>
      catalogAPI<CatalogTrending>(`/catalog/trending?${params({ period, language })}`, visitor, {
        signal,
      }),
    staleTime: 60_000,
    refetchInterval: false,
    placeholderData: keepPreviousData,
  })

  const itemsByCategory = new Map<TrendingCategoryID, CatalogPosterCardItem[]>()
  for (const category of query.data?.categories ?? []) {
    itemsByCategory.set(
      category.id,
      (category.items ?? []).map((item: CatalogTrendingItem) => ({
        id: item.id,
        title: item.title,
        cover: item.cover,
        summary: trendingItemSummary(item),
        metric: trendingMetricLabel(item),
        metricTitle: trendingMetricTitle(item),
        to: `${searchPath}/${item.id}`,
      })),
    )
  }

  function changePeriod(nextPeriod: TrendingPeriod) {
    const next = new URLSearchParams(search)
    if (nextPeriod === defaultTrendingPeriod) {
      next.delete('period')
    } else {
      next.set('period', nextPeriod)
    }
    setSearch(next, { flushSync: true, preventScrollReset: true })
  }

  return (
    <div className={visitor ? 'min-w-0 space-y-6' : 'page min-w-0'}>
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="field w-full sm:w-52">
          <Label htmlFor="trending-period">Period</Label>
          <select
            id="trending-period"
            name="period"
            className="native-select w-full"
            value={period}
            onChange={(event) => changePeriod(parseTrendingPeriod(event.target.value))}
          >
            {trendingPeriods.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
        {query.isFetching && !query.isPending && (
          <p role="status" className="help">
            Updating trending titles…
          </p>
        )}
      </div>

      <ErrorNotice error={query.error} retry={() => void query.refetch()} />

      {(!query.isError || Boolean(query.data)) && (
        <div
          className={cn(
            'min-w-0 space-y-8 transition-opacity duration-150',
            query.isPlaceholderData && query.isFetching && 'opacity-70',
          )}
        >
          {trendingCategories.map((category, index) => (
            <CatalogPosterRail
              key={category.id}
              category={category}
              items={itemsByCategory.get(category.id) ?? []}
              loading={query.isPending}
              loadingLabel={`Loading trending ${category.label.toLowerCase()}`}
              emptyMessage={`No trending ${category.label.toLowerCase()} in the ${periodLabel.toLowerCase()}.`}
              eagerRow={index === 0}
              listState={listState}
              resetScrollKey={period}
            />
          ))}
        </div>
      )}
    </div>
  )
}
