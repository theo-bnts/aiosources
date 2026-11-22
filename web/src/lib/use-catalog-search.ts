import { useQueries, useQuery } from '@tanstack/react-query'
import { params } from './api'
import { catalogAPI, catalogQueryKey, usePublicSearch } from './public-search'
import { catalogSearchPlan, hasCatalogRanking, mergeCatalogPages } from './catalog-search-results'
import type { CatalogTitle } from './catalog-search-types'
import type { Page } from './types'

export function useCatalogSearch({
  q,
  type,
  year,
  language,
  limit,
  offset,
}: {
  q: string
  type: string
  year: string
  language: string
  limit: number
  offset: number
}) {
  const visitor = usePublicSearch()
  const enabled = q.trim().length >= 2
  const plan = catalogSearchPlan(type, limit, offset)
  const prefix = catalogQueryKey(visitor, 'search')
  function read(type: string, limit: number, offset: number, signal: AbortSignal) {
    return catalogAPI<Page<CatalogTitle>>(
      `/catalog/search?${params({ q, type, year, language, limit, offset })}`,
      visitor,
      { signal },
    )
  }
  const queries = useQueries({
    queries: plan.requests.map((request) => ({
      queryKey: [
        ...prefix,
        'category',
        q,
        request.type,
        year,
        language,
        request.limit,
        request.offset,
      ],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        read(request.type, request.limit, request.offset, signal),
      enabled,
      refetchInterval: false,
    })),
  })
  const pages = queries.flatMap((query) => (query.data ? [query.data] : []))
  // Old servers or cached responses can lack ordering keys. Fall back to their
  // global ordering rather than guessing relevance or concatenating categories.
  const globalOrder =
    plan.parallel && pages.some((page) => page.items?.some((item) => !hasCatalogRanking(item)))
  const fallback = useQuery({
    queryKey: [...prefix, 'global-order', q, type, year, language, limit, offset],
    queryFn: ({ signal }) => read(type, limit, offset, signal),
    enabled: enabled && globalOrder,
    refetchInterval: false,
  })
  const empty: Page<CatalogTitle> = { items: [], total: 0, limit, offset }
  const data = globalOrder
    ? (fallback.data ?? empty)
    : plan.parallel
      ? mergeCatalogPages(pages, limit, offset)
      : (pages[0] ?? empty)
  const complete = globalOrder ? Boolean(fallback.data) : pages.length === queries.length
  const error = globalOrder ? fallback.error : queries.find((query) => query.error)?.error
  return {
    data,
    error,
    complete,
    isPending: globalOrder ? fallback.isPending : queries.some((query) => query.isPending),
    isFetching: globalOrder ? fallback.isFetching : queries.some((query) => query.isFetching),
    totalCategories: plan.categories.length,
    completedCategories: globalOrder
      ? fallback.data
        ? plan.categories.length
        : 0
      : queries.reduce(
          (total, query, index) => total + (query.data ? plan.requests[index].categories : 0),
          0,
        ),
    categoryStatus: globalOrder
      ? `Catalog: ${fallback.data ? 'complete' : fallback.isError ? 'unavailable' : 'searching'}`
      : plan.requests
          .map(
            (request, index) =>
              `${request.category}: ${queries[index].data ? 'complete' : queries[index].isError ? 'unavailable' : 'searching'}`,
          )
          .join(' · '),
    refetch() {
      if (globalOrder) void fallback.refetch()
      else
        void Promise.allSettled(
          queries.filter((query) => query.isError).map((query) => query.refetch()),
        )
    },
  }
}
