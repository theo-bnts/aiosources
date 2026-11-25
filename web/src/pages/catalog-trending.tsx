import { useCallback, useEffect, useId, useRef, useState } from 'react'
import {
  AppWindowIcon,
  BookOpenIcon,
  CaretLeftIcon,
  CaretRightIcon,
  FilmStripIcon,
  GameControllerIcon,
  MusicNotesIcon,
} from '@phosphor-icons/react'
import type { Icon } from '@phosphor-icons/react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'

import { params } from '@/lib/api'
import {
  defaultTrendingPeriod,
  parseTrendingPeriod,
  trendingCategories,
  trendingItemContext,
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
import { isAllowedCoverURL } from '@/components/catalog-cover'
import { ErrorNotice } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'

const categoryIcons: Record<TrendingCategoryID, Icon> = {
  videos: FilmStripIcon,
  games: GameControllerIcon,
  software: AppWindowIcon,
  music: MusicNotesIcon,
  books: BookOpenIcon,
}

function TrendingPosterCard({
  item,
  frame,
  icon: FallbackIcon,
  to,
  state,
  eager = false,
}: {
  item: CatalogTrendingItem
  frame: 'portrait' | 'square' | 'icon'
  icon: Icon
  to: string
  state: unknown
  eager?: boolean
}) {
  const [failedSrc, setFailedSrc] = useState('')
  const showImage = isAllowedCoverURL(item.cover) && failedSrc !== item.cover
  const context = trendingItemContext(item)
  const metric = trendingMetricLabel(item)
  const metricTitle = trendingMetricTitle(item)
  return (
    <Link
      to={to}
      state={state}
      className="group block w-36 shrink-0 snap-start rounded-lg outline-none sm:w-40"
    >
      <div
        className={cn(
          'relative overflow-hidden rounded-lg border border-border bg-card shadow-xs transition-[border-color,box-shadow] duration-200 group-hover:border-primary/60 group-hover:shadow-md',
          frame === 'portrait' ? 'aspect-[2/3]' : 'aspect-square',
        )}
      >
        {showImage ? (
          <img
            src={item.cover}
            alt=""
            loading={eager ? 'eager' : 'lazy'}
            decoding="async"
            referrerPolicy="no-referrer"
            className={cn(
              'h-full w-full transition-transform duration-300 motion-reduce:transition-none',
              frame === 'icon'
                ? 'bg-muted/40 object-contain p-7 group-hover:scale-105 motion-reduce:group-hover:scale-none'
                : 'bg-muted object-cover group-hover:scale-[1.03] motion-reduce:group-hover:scale-none',
            )}
            onError={() => setFailedSrc(item.cover ?? '')}
          />
        ) : (
          <div
            aria-hidden="true"
            className="flex h-full w-full items-center justify-center bg-muted/50 text-muted-foreground/70"
          >
            <FallbackIcon size={36} />
          </div>
        )}
      </div>
      <div className="mt-2 space-y-0.5 px-0.5">
        <p className="line-clamp-2 text-sm leading-snug font-medium text-foreground transition-colors group-hover:text-primary">
          {item.title}
        </p>
        <p title={metricTitle} className="flex items-center text-xs text-muted-foreground">
          {context ? (
            <>
              <span className="min-w-0 truncate">{context}</span>
              <span aria-hidden="true" className="shrink-0 whitespace-pre">
                {' · '}
              </span>
              <span className="shrink-0">{metric}</span>
            </>
          ) : (
            <span className="truncate">{metric}</span>
          )}
        </p>
      </div>
    </Link>
  )
}

function TrendingCategoryRail({
  category,
  items,
  periodLabel,
  loading,
  eagerRow,
  searchPath,
  listState,
}: {
  category: (typeof trendingCategories)[number]
  items: CatalogTrendingItem[]
  periodLabel: string
  loading: boolean
  eagerRow: boolean
  searchPath: string
  listState: unknown
}) {
  const headingId = useId()
  const railRef = useRef<HTMLDivElement>(null)
  const [scrollState, setScrollState] = useState({ left: false, right: false })
  const Icon = categoryIcons[category.id]

  const measureScroll = useCallback(() => {
    const node = railRef.current
    if (!node) {
      setScrollState((current) =>
        current.left || current.right ? { left: false, right: false } : current,
      )
      return
    }
    const left = node.scrollLeft > 4
    const right = node.scrollLeft + node.clientWidth < node.scrollWidth - 4
    setScrollState((current) =>
      current.left === left && current.right === right ? current : { left, right },
    )
  }, [])

  useEffect(() => {
    const node = railRef.current
    if (!node) return
    node.scrollLeft = 0
    measureScroll()
    const observer = new ResizeObserver(measureScroll)
    observer.observe(node)
    return () => observer.disconnect()
  }, [items, measureScroll])

  function scrollByDirection(direction: -1 | 1) {
    const node = railRef.current
    if (!node) return
    const distance = Math.max(Math.round(node.clientWidth * 0.8), 280)
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    node.scrollBy({
      left: direction * distance,
      behavior: reducedMotion ? 'auto' : 'smooth',
    })
  }

  const canScroll = scrollState.left || scrollState.right

  return (
    <section aria-labelledby={headingId} className="min-w-0 space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <Icon size={20} aria-hidden="true" className="shrink-0 text-primary" />
          <h2 id={headingId} className="section-title">
            {category.label}
          </h2>
        </div>
        {!loading && canScroll && (
          <div className="flex shrink-0 items-center gap-1.5">
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={!scrollState.left}
              aria-label={`Scroll ${category.label} left`}
              onClick={() => scrollByDirection(-1)}
            >
              <CaretLeftIcon aria-hidden="true" />
            </Button>
            <Button
              type="button"
              variant="outline"
              size="icon"
              disabled={!scrollState.right}
              aria-label={`Scroll ${category.label} right`}
              onClick={() => scrollByDirection(1)}
            >
              <CaretRightIcon aria-hidden="true" />
            </Button>
          </div>
        )}
      </div>

      {loading ? (
        <div
          aria-busy="true"
          aria-label={`Loading trending ${category.label.toLowerCase()}`}
          className="flex gap-4 overflow-hidden pb-2"
        >
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="w-36 shrink-0 space-y-2 sm:w-40">
              <Skeleton
                className={cn(
                  'w-full rounded-lg',
                  category.frame === 'portrait' ? 'aspect-[2/3]' : 'aspect-square',
                )}
              />
              <Skeleton className="h-4 w-4/5" />
              <Skeleton className="h-3 w-3/5" />
            </div>
          ))}
        </div>
      ) : items.length > 0 ? (
        <div
          ref={railRef}
          onScroll={measureScroll}
          className="flex min-w-0 snap-x snap-mandatory gap-4 overflow-x-auto overscroll-x-contain pb-2 [scrollbar-width:thin]"
        >
          {items.map((item, index) => (
            <TrendingPosterCard
              key={item.id}
              item={item}
              frame={category.frame}
              icon={Icon}
              to={`${searchPath}/${item.id}`}
              state={listState}
              eager={eagerRow && index < 6}
            />
          ))}
        </div>
      ) : (
        <p className="rounded-md border border-dashed border-border/80 bg-card/30 px-4 py-4 text-sm text-muted-foreground">
          No trending {category.label.toLowerCase()} in the {periodLabel.toLowerCase()}.
        </p>
      )}
    </section>
  )
}

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

  const itemsByCategory = new Map<TrendingCategoryID, CatalogTrendingItem[]>()
  for (const category of query.data?.categories ?? []) {
    itemsByCategory.set(category.id, category.items ?? [])
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
            <TrendingCategoryRail
              key={category.id}
              category={category}
              items={itemsByCategory.get(category.id) ?? []}
              periodLabel={periodLabel}
              loading={query.isPending}
              eagerRow={index === 0}
              searchPath={searchPath}
              listState={listState}
            />
          ))}
        </div>
      )}
    </div>
  )
}
