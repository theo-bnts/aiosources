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

export interface TorznabCapabilityFieldChange {
  field: TorznabCapabilityField
  label: string
  currentDisplay: string
  nextDisplay: string
  currentValue: string | number | undefined
  nextValue: string | number
  matches: boolean
  overwrites: boolean
}

export const TORZNAB_TEMPLATE_NAME = 'Example Torznab provider'
export const TORZNAB_TEMPLATE_ID = 'example_torznab'
export const TORZNAB_TEMPLATE_URL = 'https://indexer.example/api'
export const TORZNAB_TEMPLATE_PAGE_SIZE = 100
export const TORZNAB_TEMPLATE_SEARCH_MODE = 'search'

export const torznabSearchModeLabels: Record<string, string> = {
  search: 'General search',
  tvsearch: 'Series',
  movie: 'Films',
  music: 'Music',
  book: 'Books',
}

export function formatTorznabSearchMode(mode: string): string {
  return torznabSearchModeLabels[mode] ?? mode
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
  // Applying the canonical discovered endpoint URL does not invalidate the fetch.
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

export function isCanonicalEndpointUpgrade(currentURL: string, discoveredURL: string): boolean {
  const trimmed = currentURL.trim()
  if (!trimmed || trimmed === TORZNAB_TEMPLATE_URL) return true
  try {
    const current = new URL(trimmed)
    const discovered = new URL(discoveredURL)
    if (current.origin !== discovered.origin) return false
    const currentPath = current.pathname.replace(/\/+$/, '')
    const discoveredPath = discovered.pathname.replace(/\/+$/, '')
    // Same path with reserved query params cleaned (e.g. ?t=caps) or base URL -> /api discovery.
    if (currentPath === discoveredPath) return true
    if (currentPath === '' || discoveredPath.startsWith(`${currentPath}/`)) return true
    return false
  } catch {
    return false
  }
}

export function isTorznabTemplatePlaceholderURL(url: unknown): boolean {
  return typeof url !== 'string' || url.trim() === '' || url.trim() === TORZNAB_TEMPLATE_URL
}

export function planTorznabCapabilityChanges(
  document: ProviderJSON,
  caps: TorznabCapabilitiesResult,
  options: { isExistingSource: boolean },
): TorznabCapabilityFieldChange[] {
  const changes: TorznabCapabilityFieldChange[] = []
  const { isExistingSource } = options

  const title = caps.title?.trim() ?? ''
  if (title) {
    const current = typeof document.get('name') === 'string' ? document.get('name') : ''
    const currentText = typeof current === 'string' ? current : ''
    const matches = currentText === title
    const overwrites =
      !matches &&
      currentText.trim() !== '' &&
      (isExistingSource || currentText !== TORZNAB_TEMPLATE_NAME)
    changes.push({
      field: 'name',
      label: 'Display name',
      currentDisplay: currentText || 'Not set',
      nextDisplay: title,
      currentValue: currentText || undefined,
      nextValue: title,
      matches,
      overwrites,
    })
  }

  const suggestedID = caps.suggested_id?.trim() ?? ''
  if (!isExistingSource && suggestedID) {
    const current = typeof document.get('id') === 'string' ? document.get('id') : ''
    const currentText = typeof current === 'string' ? current : ''
    const matches = currentText === suggestedID
    const overwrites = !matches && currentText.trim() !== '' && currentText !== TORZNAB_TEMPLATE_ID
    changes.push({
      field: 'id',
      label: 'Source identifier',
      currentDisplay: currentText || 'Not set',
      nextDisplay: suggestedID,
      currentValue: currentText || undefined,
      nextValue: suggestedID,
      matches,
      overwrites,
    })
  }

  const endpoint = caps.endpoint?.trim() ?? ''
  if (endpoint) {
    const current = typeof document.get('url') === 'string' ? document.get('url') : ''
    const currentText = typeof current === 'string' ? current : ''
    const matches = currentText === endpoint
    const overwrites =
      !matches &&
      currentText.trim() !== '' &&
      (isExistingSource || !isCanonicalEndpointUpgrade(currentText, endpoint))
    changes.push({
      field: 'url',
      label: 'Source URL',
      currentDisplay: currentText || 'Not set',
      nextDisplay: endpoint,
      currentValue: currentText || undefined,
      nextValue: endpoint,
      matches,
      overwrites,
    })
  }

  const recommendedPageSize = caps.recommended_page_size ?? 0
  if (recommendedPageSize > 0) {
    const rawPageSize = document.get('page_size')
    const current =
      typeof rawPageSize === 'number' && Number.isInteger(rawPageSize) ? rawPageSize : undefined
    const matches = current === recommendedPageSize
    const overwrites =
      !matches &&
      current !== undefined &&
      (isExistingSource || current !== TORZNAB_TEMPLATE_PAGE_SIZE)
    const sourceLabel =
      caps.limits.max && caps.limits.max > 0
        ? `${recommendedPageSize} (server max)`
        : `${recommendedPageSize} (server default)`
    changes.push({
      field: 'page_size',
      label: 'Page size',
      currentDisplay: current !== undefined ? String(current) : 'Default (100)',
      nextDisplay: sourceLabel,
      currentValue: current,
      nextValue: recommendedPageSize,
      matches,
      overwrites,
    })
  }

  const recommendedMode = caps.recommended_search_mode?.trim() ?? ''
  if (recommendedMode) {
    const rawMode = document.getIn(['options', 'search_mode'])
    const currentExplicit = typeof rawMode === 'string' ? rawMode : ''
    const effectiveMode = currentExplicit || TORZNAB_TEMPLATE_SEARCH_MODE
    const advertisedCurrent = caps.searches.find((item) => item.mode === effectiveMode)
    // Keep a working user-selected search mode (e.g. movie/tvsearch) intact unless it is
    // explicitly advertised unavailable by the server.
    const currentSupported = advertisedCurrent ? advertisedCurrent.available : true
    const matches =
      effectiveMode === recommendedMode || (currentExplicit !== '' && currentSupported)
    const overwrites =
      !matches &&
      (isExistingSource ||
        (currentExplicit !== '' && currentExplicit !== TORZNAB_TEMPLATE_SEARCH_MODE))
    const currentDisplay = currentExplicit
      ? advertisedCurrent && !advertisedCurrent.available
        ? `${formatTorznabSearchMode(currentExplicit)} (unavailable)`
        : formatTorznabSearchMode(currentExplicit)
      : 'General search (default)'
    changes.push({
      field: 'search_mode',
      label: 'Search mode',
      currentDisplay,
      nextDisplay: formatTorznabSearchMode(recommendedMode),
      currentValue: currentExplicit || undefined,
      nextValue: recommendedMode,
      matches,
      overwrites,
    })
  }

  return changes
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

export function applyTorznabCapabilityChanges(
  next: ProviderJSON,
  changes: readonly TorznabCapabilityFieldChange[],
  options: { includeOverwrites: boolean },
): TorznabCapabilityFieldChange[] {
  const applied: TorznabCapabilityFieldChange[] = []
  for (const item of changes) {
    if (item.matches) continue
    if (item.overwrites && !options.includeOverwrites) continue
    applyTorznabCapabilityField(next, item.field, item.nextValue)
    applied.push(item)
  }
  return applied
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

export function flattenTorznabCategoryIDs(
  categories: readonly TorznabCategoryCapability[],
): Set<number> {
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
