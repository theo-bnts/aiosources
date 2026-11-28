import { useCallback, useEffect, useId, useRef, useState } from 'react'
import {
  AppWindowIcon,
  BellIcon,
  BookOpenIcon,
  CaretLeftIcon,
  CaretRightIcon,
  FilmStripIcon,
  GameControllerIcon,
  MagnifyingGlassIcon,
  MusicNotesIcon,
  PlusIcon,
  WarningCircleIcon,
} from '@phosphor-icons/react'
import type { Icon } from '@phosphor-icons/react'
import { Link } from 'react-router-dom'

import type { CatalogCardFrame, CatalogCategoryID } from '@/lib/catalog-cards'
import { cn } from '@/lib/utils'
import { isAllowedCoverURL } from '@/components/catalog-cover'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

export type CatalogPosterCardItem = {
  id: number
  key?: string
  title: string
  cover?: string
  summary: string
  metric: string
  metricTitle?: string
  to: string
  onSelect?: () => void
  alertActive?: boolean
  alertStatus?: 'watching' | 'retrying'
}

const categoryIcons: Record<CatalogCategoryID | 'catalog', Icon> = {
  videos: FilmStripIcon,
  games: GameControllerIcon,
  software: AppWindowIcon,
  music: MusicNotesIcon,
  books: BookOpenIcon,
  catalog: MagnifyingGlassIcon,
}

export function CatalogPosterCard({
  item,
  frame,
  icon: FallbackIcon,
  state,
  eager = false,
}: {
  item: CatalogPosterCardItem
  frame: CatalogCardFrame
  icon: Icon
  state: unknown
  eager?: boolean
}) {
  const titleId = useId()
  const metaId = useId()
  const [failedSrc, setFailedSrc] = useState('')
  const showImage = isAllowedCoverURL(item.cover) && failedSrc !== item.cover
  const cardBody = (
    <>
      <div
        data-slot="catalog-card-frame"
        data-frame={frame}
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
            <FallbackIcon size={36} aria-hidden="true" />
          </div>
        )}
        {(item.alertStatus === 'retrying' ||
          item.alertStatus === 'watching' ||
          item.alertActive) && (
          <span
            title={
              item.alertStatus === 'retrying' ? 'Alert delivery retrying' : 'Release alert active'
            }
            aria-label={
              item.alertStatus === 'retrying' ? 'Alert delivery retrying' : 'Release alert active'
            }
            className={cn(
              'absolute top-2 right-2 flex size-7 items-center justify-center rounded-full border border-border/70 bg-background/90 shadow-xs backdrop-blur-xs',
              item.alertStatus === 'retrying' ? 'text-destructive' : 'text-primary',
            )}
          >
            {item.alertStatus === 'retrying' ? (
              <WarningCircleIcon size={15} weight="fill" aria-hidden="true" />
            ) : (
              <BellIcon size={15} weight="fill" aria-hidden="true" />
            )}
          </span>
        )}
      </div>
      <div className="mt-2 space-y-0.5 px-0.5">
        <p
          id={titleId}
          data-slot="catalog-card-title"
          className="line-clamp-2 text-sm leading-snug font-medium text-foreground transition-colors group-hover:text-primary"
        >
          {item.title}
        </p>
        <div id={metaId} className="space-y-0.5 text-xs text-muted-foreground">
          <p className="truncate">{item.summary}</p>
          <p title={item.metricTitle} className="truncate text-muted-foreground/80">
            {item.metric}
          </p>
        </div>
      </div>
    </>
  )
  if (item.onSelect) {
    return (
      <button
        type="button"
        onClick={item.onSelect}
        aria-labelledby={titleId}
        aria-describedby={metaId}
        data-slot="catalog-card"
        className="group block w-36 shrink-0 cursor-pointer snap-start rounded-lg text-left outline-none sm:w-40"
      >
        {cardBody}
      </button>
    )
  }
  return (
    <Link
      to={item.to}
      state={state}
      aria-labelledby={titleId}
      aria-describedby={metaId}
      data-slot="catalog-card"
      className="group block w-36 shrink-0 snap-start rounded-lg outline-none sm:w-40"
    >
      {cardBody}
    </Link>
  )
}

export function CatalogPosterRail({
  category,
  items,
  loading,
  loadingLabel,
  emptyMessage,
  countLabel,
  eagerRow,
  listState,
  resetScrollKey,
  dimmed = false,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
}: {
  category: { id: CatalogCategoryID | 'catalog'; label: string; frame: CatalogCardFrame }
  items: CatalogPosterCardItem[]
  loading: boolean
  loadingLabel: string
  emptyMessage?: string
  countLabel?: string
  eagerRow: boolean
  listState: unknown
  resetScrollKey: string
  dimmed?: boolean
  hasMore?: boolean
  loadingMore?: boolean
  onLoadMore?: () => void
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
  }, [resetScrollKey, measureScroll])

  useEffect(() => {
    const node = railRef.current
    if (!node) return
    measureScroll()
    const observer = new ResizeObserver(measureScroll)
    observer.observe(node)
    return () => observer.disconnect()
  }, [items, hasMore, measureScroll])

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
    <section
      aria-labelledby={headingId}
      data-slot="catalog-rail"
      data-category={category.id}
      className="min-w-0 space-y-3"
    >
      <div className="flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <Icon size={20} aria-hidden="true" className="shrink-0 text-primary" />
          <h2 id={headingId} className="section-title">
            {category.label}
          </h2>
          {countLabel && !loading && (
            <span className="text-xs text-muted-foreground tabular-nums">{countLabel}</span>
          )}
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
        <div aria-busy="true" aria-label={loadingLabel} className="flex gap-4 overflow-hidden pb-2">
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
              <Skeleton className="h-3 w-2/5" />
            </div>
          ))}
        </div>
      ) : items.length > 0 ? (
        <div
          ref={railRef}
          onScroll={measureScroll}
          className={cn(
            'flex min-w-0 snap-x snap-mandatory gap-4 overflow-x-auto overscroll-x-contain pb-2 transition-opacity duration-150 [scrollbar-width:thin]',
            dimmed && 'opacity-70',
          )}
        >
          {items.map((item, index) => (
            <CatalogPosterCard
              key={item.key ?? item.id}
              item={item}
              frame={category.frame}
              icon={Icon}
              state={listState}
              eager={eagerRow && index < 6}
            />
          ))}
          {hasMore && onLoadMore && (
            <div className="w-36 shrink-0 snap-start sm:w-40">
              <button
                type="button"
                disabled={loadingMore}
                aria-label={`Show more ${category.label}`}
                onClick={onLoadMore}
                className={cn(
                  'flex w-full flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-card/40 px-3 text-center text-sm font-medium text-muted-foreground transition-colors hover:border-primary/60 hover:bg-card hover:text-foreground disabled:pointer-events-none disabled:opacity-50',
                  category.frame === 'portrait' ? 'aspect-[2/3]' : 'aspect-square',
                )}
              >
                <PlusIcon size={24} aria-hidden="true" />
                <span>{loadingMore ? 'Loading…' : 'Show more'}</span>
              </button>
            </div>
          )}
        </div>
      ) : emptyMessage ? (
        <p className="rounded-md border border-dashed border-border/80 bg-card/30 px-4 py-4 text-sm text-muted-foreground">
          {emptyMessage}
        </p>
      ) : null}
    </section>
  )
}
