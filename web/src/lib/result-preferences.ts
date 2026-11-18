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
    const normalized: SortRule[] = []
    for (const rule of value) {
      if (
        !rule ||
        typeof rule !== 'object' ||
        Array.isArray(rule) ||
        Object.keys(rule).some((key) => key !== 'field' && key !== 'direction') ||
        !sortFields.some((field) => field.value === (rule as SortRule).field) ||
        !['asc', 'desc'].includes((rule as SortRule).direction)
      )
        return []
      const typed = rule as SortRule
      const key = ['age', 'published'].includes(typed.field) ? 'date' : typed.field
      if (seen.has(key)) return []
      seen.add(key)
      normalized.push({ field: typed.field, direction: typed.direction })
    }
    return normalized
  } catch {
    return []
  }
}

export function readResponseSeconds(raw: string | null, max = 60): number | undefined {
  if (!raw || !/^\d+$/.test(raw.trim())) return undefined
  const value = Number(raw.trim())
  return Number.isSafeInteger(value) && value >= 1 && value <= max ? value : undefined
}

export function resultPreferencesError(options: ResultPreferences, maxSeconds = 60): string {
  if (
    options.max_results !== undefined &&
    (!Number.isInteger(options.max_results) ||
      options.max_results < 0 ||
      options.max_results > 10000)
  )
    return 'Enter a maximum result count between 1 and 10,000.'
  if (
    options.max_cached !== undefined &&
    (!Number.isInteger(options.max_cached) || options.max_cached < 0 || options.max_cached > 10000)
  )
    return 'Enter a maximum cached result count between 1 and 10,000.'
  if (
    options.response_seconds !== undefined &&
    (!Number.isInteger(options.response_seconds) ||
      options.response_seconds < 0 ||
      options.response_seconds > maxSeconds)
  )
    return `Enter a response timeout between 1 and ${maxSeconds} seconds.`
  return ''
}

export function encodeBase64URL(value: unknown): string {
  const data = new TextEncoder().encode(JSON.stringify(value))
  return btoa(Array.from(data, (byte) => String.fromCharCode(byte)).join(''))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replace(/=+$/, '')
}
