import type { ProviderJSON } from './provider-json.ts'

export type TorznabSearchMode = 'search' | 'tvsearch' | 'movie' | 'music' | 'book'

export interface TorznabLimits {
  default?: number
  max?: number
}

export interface TorznabSearchModeCapability {
  mode: TorznabSearchMode
  available: boolean
  supported_params: string[]
}

export interface TorznabCategoryCapability {
  id: number
  name: string
  subcategories?: TorznabCategoryCapability[]
}

export interface TorznabRateLimitHint {
  limit?: number
  remaining?: number
  reset_at?: string
}

export interface TorznabCapabilitiesResult {
  endpoint: string
  title?: string
  suggested_id?: string
  limits: TorznabLimits
  recommended_page_size?: number
  searches: TorznabSearchModeCapability[]
  recommended_search_mode?: string
  categories: TorznabCategoryCapability[]
  rate_limit?: TorznabRateLimitHint
}

export type TorznabCapabilityField = 'name' | 'id' | 'url' | 'page_size' | 'search_mode'

const TORZNAB_TEMPLATE_URL = 'https://indexer.example/api'

const torznabSearchModeLabels: Record<string, string> = {
  search: 'General search',
  tvsearch: 'Series',
  movie: 'Films',
  music: 'Music',
  book: 'Books',
}

export function torznabSearchModeOptionLabels(
  caps: TorznabCapabilitiesResult | null,
): Record<string, string> {
  const labels: Record<string, string> = { ...torznabSearchModeLabels }
  if (!caps) return labels
  for (const item of caps.searches) {
    if (!item.available && labels[item.mode]) {
      labels[item.mode] = `${labels[item.mode]} (unavailable)`
    }
  }
  return labels
}

export function torznabConnectionKey(document: ProviderJSON, urlOverride?: string): string {
  const url = urlOverride ?? (typeof document.get('url') === 'string' ? document.get('url') : '')
  return JSON.stringify({
    adapter: document.get('adapter') ?? '',
    url: typeof url === 'string' ? url.trim() : '',
    auth: document.rawIn(['auth']),
    headers: document.rawIn(['http', 'headers']),
    secretHeaders: document.rawIn(['http', 'secret_headers']),
    query: document.rawIn(['http', 'query']),
    replaceControls: document.rawIn(['options', 'replace_invalid_xml_controls']),
  })
}

export function isTorznabCapabilitiesStale(
  fetchedKey: string | null,
  document: ProviderJSON,
  caps: TorznabCapabilitiesResult | null,
): boolean {
  if (!fetchedKey || !caps) return false
  const current = torznabConnectionKey(document)
  if (current === fetchedKey) return false
  // Applying the canonical discovered endpoint URL does not invalidate the cache.
  if (caps.endpoint && torznabConnectionKey(document, '') === stripURLFromKey(fetchedKey)) {
    const rawURL = document.get('url')
    const currentURL = typeof rawURL === 'string' ? rawURL.trim() : ''
    if (currentURL === caps.endpoint) return false
  }
  return true
}

function stripURLFromKey(key: string): string {
  try {
    const parsed = JSON.parse(key) as Record<string, unknown>
    return JSON.stringify({ ...parsed, url: '' })
  } catch {
    return key
  }
}

export function isTorznabTemplatePlaceholderURL(url: unknown): boolean {
  return typeof url !== 'string' || url.trim() === '' || url.trim() === TORZNAB_TEMPLATE_URL
}

export function resolveTorznabCapabilityFieldValue(
  caps: TorznabCapabilitiesResult,
  field: TorznabCapabilityField,
): { value: string | number; label: string; missingMessage: string } | null {
  switch (field) {
    case 'name': {
      const title = caps.title?.trim() ?? ''
      return title
        ? {
            value: title,
            label: 'Display name',
            missingMessage: 'This server did not advertise a display name.',
          }
        : null
    }
    case 'id': {
      const id = caps.suggested_id?.trim() ?? ''
      return id
        ? {
            value: id,
            label: 'Source identifier',
            missingMessage: 'Could not derive a source identifier from capabilities.',
          }
        : null
    }
    case 'url': {
      const endpoint = caps.endpoint?.trim() ?? ''
      return endpoint
        ? {
            value: endpoint,
            label: 'Source URL',
            missingMessage: 'This server did not return an endpoint URL.',
          }
        : null
    }
    case 'page_size': {
      const raw = caps.recommended_page_size || caps.limits.max || caps.limits.default || 0
      const clamped = raw > 0 ? Math.min(raw, 10000) : 0
      return clamped > 0
        ? {
            value: clamped,
            label: 'Page size',
            missingMessage: 'This server did not advertise page-size limits.',
          }
        : null
    }
    case 'search_mode': {
      const mode = caps.recommended_search_mode?.trim() ?? ''
      return mode
        ? {
            value: mode,
            label: 'Search mode',
            missingMessage: 'This server did not advertise supported search modes.',
          }
        : null
    }
  }
}

export function readTorznabCapabilityFieldValue(
  document: ProviderJSON,
  field: TorznabCapabilityField,
): string | number | undefined {
  switch (field) {
    case 'name': {
      const value = document.get('name')
      return typeof value === 'string' ? value : undefined
    }
    case 'id': {
      const value = document.get('id')
      return typeof value === 'string' ? value : undefined
    }
    case 'url': {
      const value = document.get('url')
      return typeof value === 'string' ? value : undefined
    }
    case 'page_size': {
      const value = document.get('page_size')
      return typeof value === 'number' && Number.isInteger(value) ? value : undefined
    }
    case 'search_mode': {
      const value = document.getIn(['options', 'search_mode'])
      return typeof value === 'string' ? value : undefined
    }
  }
}

export function applyTorznabCapabilityField(
  next: ProviderJSON,
  field: TorznabCapabilityField,
  value: string | number,
): void {
  switch (field) {
    case 'name':
      next.setIn(['name'], String(value))
      break
    case 'id':
      next.setIn(['id'], String(value))
      break
    case 'url':
      next.setIn(['url'], String(value))
      break
    case 'page_size':
      next.setIn(['page_size'], Number(value))
      break
    case 'search_mode':
      next.setIn(['options', 'search_mode'], String(value))
      break
  }
}

export function readSelectedCategoryIDs(document: ProviderJSON): number[] {
  const value = document.getIn(['search', 'categories'])
  if (!Array.isArray(value)) return []
  const seen = new Set<number>()
  const ids: number[] = []
  for (const entry of value) {
    if (typeof entry === 'number' && Number.isInteger(entry) && entry >= 0 && !seen.has(entry)) {
      seen.add(entry)
      ids.push(entry)
    }
  }
  return ids
}

export function applyTorznabCategories(
  next: ProviderJSON,
  ids: readonly number[],
): { switchedScope?: 'combined' } {
  const sorted = [...new Set(ids)]
    .filter((id) => Number.isInteger(id) && id >= 0)
    .sort((a, b) => a - b)
  const scope = next.getIn(['options', 'category_scope'])
  next.setIn(['search', 'categories'], sorted)
  if (sorted.length > 0 && scope === 'advertised') {
    next.setIn(['options', 'category_scope'], 'combined')
    return { switchedScope: 'combined' }
  }
  if (sorted.length === 0 && scope === 'each') {
    next.setIn(['options', 'category_scope'], 'combined')
    return { switchedScope: 'combined' }
  }
  return {}
}

export function applyUseAdvertisedCategories(next: ProviderJSON): void {
  next.setIn(['options', 'category_scope'], 'advertised')
  next.setIn(['search', 'categories'], [])
}

function flattenTorznabCategoryIDs(categories: readonly TorznabCategoryCapability[]): Set<number> {
  const ids = new Set<number>()
  const visit = (items: readonly TorznabCategoryCapability[]) => {
    for (const item of items) {
      ids.add(item.id)
      if (item.subcategories?.length) visit(item.subcategories)
    }
  }
  visit(categories)
  return ids
}

export function countTorznabCategories(categories: readonly TorznabCategoryCapability[]): number {
  return flattenTorznabCategoryIDs(categories).size
}

export function findUnknownCategoryIDs(
  selectedIDs: readonly number[],
  categories: readonly TorznabCategoryCapability[],
): number[] {
  const known = flattenTorznabCategoryIDs(categories)
  return selectedIDs.filter((id) => !known.has(id))
}

export function filterTorznabCategories(
  categories: readonly TorznabCategoryCapability[],
  query: string,
): TorznabCategoryCapability[] {
  const needle = query.trim().toLowerCase()
  if (!needle) return [...categories]
  const matchesNode = (category: TorznabCategoryCapability) =>
    category.name.toLowerCase().includes(needle) || String(category.id).includes(needle)
  const filterList = (items: readonly TorznabCategoryCapability[]): TorznabCategoryCapability[] => {
    const result: TorznabCategoryCapability[] = []
    for (const item of items) {
      if (matchesNode(item)) {
        result.push(item)
        continue
      }
      if (item.subcategories?.length) {
        const matchedChildren = filterList(item.subcategories)
        if (matchedChildren.length > 0) {
          result.push({ ...item, subcategories: matchedChildren })
        }
      }
    }
    return result
  }
  return filterList(categories)
}
