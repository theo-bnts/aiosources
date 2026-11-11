import { catalogMediaTypes } from './connection-options.ts'
import type { CatalogTitle } from './catalog-search-types.ts'
import type { Page } from './types.ts'

export type CatalogSearchRequest = {
  category: string
  type: string
  limit: number
  offset: number
  categories: number
}

// Bound the combined window by the API's existing page limit. A deep link or
// distant page keeps the global SQL page instead of downloading its entire prefix.
const maximumSearchWindow = 200

export function catalogSearchPlan(type: string, limit: number, offset: number) {
  const selected = type ? [...new Set(type.split(',').map((value) => value.trim()))] : []
  const valid = selected.every((value) =>
    catalogMediaTypes.some((option) => option.value === value),
  )
  const groups = [...new Set(catalogMediaTypes.map((option) => option.group || 'Catalog'))]
  const categories = valid
    ? groups.flatMap((group) => {
        const values = catalogMediaTypes
          .filter(
            (option) =>
              (option.group || 'Catalog') === group &&
              (!selected.length || selected.includes(option.value)),
          )
          .map((option) => option.value)
        return values.length ? [{ category: group, type: values.join(',') }] : []
      })
    : [{ category: 'Catalog', type }]
  const parallel = categories.length > 1 && offset <= maximumSearchWindow - limit
  const requests: CatalogSearchRequest[] = parallel
    ? categories.map((category) => ({
        ...category,
        limit: offset + limit,
        offset: 0,
        categories: 1,
      }))
    : [
        {
          category: categories.map((item) => item.category).join(', '),
          type,
          limit,
          offset,
          categories: categories.length,
        },
      ]
  return { requests, parallel, categories }
}

export function hasCatalogRanking(title: CatalogTitle) {
  return Boolean(
    title.ranking &&
    Number.isInteger(title.ranking.precision) &&
    Number.isFinite(title.ranking.relevance) &&
    Number.isInteger(title.ranking.year),
  )
}

// Use the exact SQL keys, including its stored work year rather than a localized
// edition's displayed year. Labels, arrival order and cover availability do not rank results.
export function compareCatalogTitles(a: CatalogTitle, b: CatalogTitle) {
  const left = a.ranking!,
    right = b.ranking!
  return (
    right.precision - left.precision ||
    right.relevance - left.relevance ||
    right.year - left.year ||
    a.id - b.id
  )
}

export function mergeCatalogPages(
  pages: Page<CatalogTitle>[],
  limit: number,
  offset: number,
): Page<CatalogTitle> {
  const items = pages.flatMap((page) => page.items ?? []).sort(compareCatalogTitles)
  return {
    items: items.slice(offset, offset + limit),
    total: pages.reduce((sum, page) => sum + page.total, 0),
    limit,
    offset,
  }
}
