export type SortField = 'cached' | 'language' | 'size' | 'published' | 'age' | 'name' | 'source'
export type SortRule = { field: SortField; direction: 'asc' | 'desc' }
export type ResultPreferences = {
  sorting?: SortRule[]
  max_results?: number
  max_cached?: number
  response_seconds?: number
}

export const sortFields: { value: SortField; label: string; direction: SortRule['direction'] }[] = [
  { value: 'cached', label: 'Cached status', direction: 'desc' },
  { value: 'language', label: 'Preferred language', direction: 'desc' },
  { value: 'size', label: 'Size', direction: 'desc' },
  { value: 'published', label: 'Publication date', direction: 'desc' },
  { value: 'age', label: 'Age', direction: 'asc' },
  { value: 'name', label: 'Name', direction: 'asc' },
  { value: 'source', label: 'Source', direction: 'asc' },
]

export function resultPreferences(value: ResultPreferences): ResultPreferences {
  return {
    ...(value.sorting?.length ? { sorting: value.sorting } : {}),
    ...(value.max_results ? { max_results: value.max_results } : {}),
    ...(value.max_cached ? { max_cached: value.max_cached } : {}),
    ...(value.response_seconds ? { response_seconds: value.response_seconds } : {}),
  }
}

export function readResultSorting(raw: string | null): SortRule[] {
  try {
    const value: unknown = JSON.parse(raw || '[]')
    if (!Array.isArray(value) || value.length > 3) return []
    const seen = new Set<string>()
    for (const rule of value) {
      if (
        !rule ||
        !sortFields.some((field) => field.value === rule.field) ||
        !['asc', 'desc'].includes(rule.direction)
      )
        return []
      const key = ['age', 'published'].includes(rule.field) ? 'date' : rule.field
      if (seen.has(key)) return []
      seen.add(key)
    }
    return value
  } catch {
    return []
  }
}
