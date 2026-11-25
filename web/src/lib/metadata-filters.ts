import { identificationResults } from './identification.ts'
import type { IdentificationStatus, MetadataFilters, Provider } from './types.ts'
import type { MultiSelectOption } from './multi-select-values.ts'

const encoder = new TextEncoder()

export const MAX_METADATA_FILTER_CATEGORIES = 100
export const MAX_METADATA_FILTER_CATEGORY_BYTES = 256

export const metadataIdentificationOrder: readonly IdentificationStatus[] = [
  'unmatched',
  'ambiguous',
  'pending',
  'matched',
]

export const metadataIdentificationOptions: MultiSelectOption[] = metadataIdentificationOrder.map(
  (status) => ({
    value: status,
    label: identificationResults[status].label,
  }),
)

function hasControlChar(value: string) {
  for (let i = 0; i < value.length; i++) {
    const code = value.charCodeAt(i)
    if (code < 0x20 || code === 0x7f) return true
  }
  return false
}

export function parseMetadataCategories(raw: string): { values: string[]; error: string } {
  if (!raw.trim()) return { values: [], error: '' }
  const seen = new Set<string>()
  const values: string[] = []
  for (const part of raw.split(/[\n,]/)) {
    const value = part.trim()
    if (!value) continue
    if (encoder.encode(value).length > MAX_METADATA_FILTER_CATEGORY_BYTES) {
      return {
        values: [],
        error: `Each category must be at most ${MAX_METADATA_FILTER_CATEGORY_BYTES} bytes.`,
      }
    }
    if (hasControlChar(value)) {
      return { values: [], error: 'Categories cannot contain control characters.' }
    }
    const key = value.toLowerCase()
    if (!seen.has(key)) {
      seen.add(key)
      values.push(value)
    }
  }
  if (values.length > MAX_METADATA_FILTER_CATEGORIES) {
    return {
      values: [],
      error: `Enter at most ${MAX_METADATA_FILTER_CATEGORIES} categories.`,
    }
  }
  return { values, error: '' }
}

export function buildMetadataFilters(
  categories: string[],
  statuses: IdentificationStatus[],
): MetadataFilters | undefined {
  if (categories.length === 0 && statuses.length === 0) return undefined
  return {
    ...(categories.length > 0 ? { categories } : {}),
    ...(statuses.length > 0 ? { identification_statuses: statuses } : {}),
  }
}

export function formatIdentificationStatuses(statuses?: readonly IdentificationStatus[]): string {
  if (!statuses?.length) return ''
  return statuses.map((status) => identificationResults[status]?.label ?? status).join(', ')
}

export function formatMetadataFiltersSummary(filters?: MetadataFilters): string[] {
  if (!filters) return []
  const parts: string[] = []
  if (filters.identification_statuses?.length) {
    parts.push(`Identification: ${formatIdentificationStatuses(filters.identification_statuses)}`)
  }
  if (filters.categories?.length) {
    parts.push(`Categories: ${filters.categories.join(', ')}`)
  }
  return parts
}

function collectCategoryTokens(value: unknown, add: (token: string) => void) {
  if (typeof value === 'string' || typeof value === 'number') {
    add(String(value))
    return
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      if (typeof item === 'string' || typeof item === 'number') add(String(item))
    }
  }
}

export function suggestedProviderCategories(provider?: Provider): string[] {
  if (!provider) return []
  const seen = new Set<string>()
  const values: string[] = []
  const add = (raw: string) => {
    const value = raw.trim()
    if (!value || encoder.encode(value).length > MAX_METADATA_FILTER_CATEGORY_BYTES || hasControlChar(value)) {
      return
    }
    const key = value.toLowerCase()
    if (!seen.has(key) && values.length < 24) {
      seen.add(key)
      values.push(value)
    }
  }
  for (const scope of provider.traversal?.scopes ?? []) {
    collectCategoryTokens(scope.match?.category, add)
    collectCategoryTokens(scope.match?.categories, add)
  }
  for (const category of provider.search?.categories ?? []) {
    add(String(category))
  }
  return values
}
