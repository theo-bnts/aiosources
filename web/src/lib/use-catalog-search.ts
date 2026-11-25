import { useState } from 'react'
import { useQueries } from '@tanstack/react-query'
import { params } from './api'
import { catalogAPI, catalogQueryKey, usePublicSearch } from './public-search'
import {
  catalogSearchCategories,
  DEFAULT_SEARCH_RAIL_LIMIT,
  MAX_SEARCH_RAIL_LIMIT,
  nextSearchRailLimit,
  type CatalogSearchCategoryRequest,
} from './catalog-search-results'
import type { CatalogTitle } from './catalog-search-types'
import type { Page } from './types'

export type CatalogSearchSection = CatalogSearchCategoryRequest & {
  items: CatalogTitle[]
  total: number
  limit: number
  isPending: boolean
  isFetching: boolean
  isPlaceholderData: boolean
  isError: boolean
  error: unknown
  hasMore: boolean
  loadMore: () => void
}

export function useCatalogSearch({
  q,
  type,
  year,
  language,
}: {
  q: string
  type: string
  year: string
  language: string
}) {
  const visitor = usePublicSearch()
  const enabled = q.trim().length >= 2
  const categories = catalogSearchCategories(type)
  const prefix = catalogQueryKey(visitor, 'search')
  const scopeKey = `${prefix.join('\0')}\0${q}\0${type}\0${year}\0${language}`
  const [extraLimits, setExtraLimits] = useState<{ scope: string; values: Record<string, number> }>(
    {
      scope: scopeKey,
      values: {},
    },
  )
  const limits = extraLimits.scope === scopeKey ? extraLimits.values : {}

  function read(categoryType: string, limit: number, signal: AbortSignal) {
    return catalogAPI<Page<CatalogTitle>>(
      `/catalog/search?${params({ q, type: categoryType, year, language, limit, offset: 0 })}`,
      visitor,
      { signal },
    )
  }

  const queries = useQueries({
    queries: categories.map((category) => {
      const limit = limits[category.id] ?? DEFAULT_SEARCH_RAIL_LIMIT
      return {
        queryKey: [...prefix, 'rail', q, category.type, year, language, limit] as const,
        queryFn: ({ signal }: { signal: AbortSignal }) => read(category.type, limit, signal),
        enabled,
        refetchInterval: false as const,
        // Keep currently rendered cards only while expanding the exact same category
        // via "Show more"; changing query, type, or year clears stale results immediately.
        placeholderData: (
          previousData: Page<CatalogTitle> | undefined,
          previousQuery: { queryKey: readonly unknown[] } | undefined,
        ) => {
          const prev = previousQuery?.queryKey
          if (!prev) return undefined
          const base = prefix.length
          if (
            prev.length === base + 6 &&
            prev[base] === 'rail' &&
            prev[base + 1] === q &&
            prev[base + 2] === category.type &&
            prev[base + 3] === year &&
            prev[base + 4] === language
          ) {
            return previousData
          }
          return undefined
        },
      }
    }),
  })

  const sections: CatalogSearchSection[] = categories.map((category, index) => {
    const query = queries[index]
    const limit = limits[category.id] ?? DEFAULT_SEARCH_RAIL_LIMIT
    const items = query.data?.items ?? []
    const total = query.data?.total ?? 0
    return {
      ...category,
      items,
      total,
      limit,
      isPending: query.isPending,
      isFetching: query.isFetching,
      isPlaceholderData: query.isPlaceholderData,
      isError: query.isError,
      error: query.error,
      hasMore: total > items.length && limit < MAX_SEARCH_RAIL_LIMIT,
      loadMore: () => {
        setExtraLimits((current) => {
          const baseValues = current.scope === scopeKey ? current.values : {}
          const nextLimit = nextSearchRailLimit(
            baseValues[category.id] ?? DEFAULT_SEARCH_RAIL_LIMIT,
          )
          return {
            scope: scopeKey,
            values: { ...baseValues, [category.id]: nextLimit },
          }
        })
      },
    }
  })

  const total = sections.reduce((sum, section) => sum + section.total, 0)
  const completedCategories = queries.reduce((sum, query) => sum + (query.data ? 1 : 0), 0)
  const complete = completedCategories === categories.length
  const error = queries.find((query) => query.error)?.error

  return {
    sections,
    total,
    error,
    complete,
    isPending: queries.some((query) => query.isPending),
    isFetching: queries.some((query) => query.isFetching),
    totalCategories: categories.length,
    completedCategories,
    categoryStatus: categories
      .map(
        (category, index) =>
          `${category.label}: ${queries[index].data ? 'complete' : queries[index].isError ? 'unavailable' : 'searching'}`,
      )
      .join(' · '),
    refetch() {
      void Promise.allSettled(
        queries.filter((query) => query.isError).map((query) => query.refetch()),
      )
    },
  }
}
