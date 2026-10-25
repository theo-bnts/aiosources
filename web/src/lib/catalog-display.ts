import type { CatalogShare, ConnectionFormat } from './catalog-types'
import { stremioMediaTypes, torznabMediaTypes } from './connection-options.ts'
import { serviceNames } from './debrid.ts'
import { number } from './format.ts'
import { resolvedLanguage } from './language-preferences.ts'
import { sortFields } from './result-preferences.ts'

const publishedFields: Record<string, string> = {
  title: 'Title',
  size: 'Size',
  info_hash: 'Info hash',
  published_at: 'Publication date',
  category: 'Category',
  categories: 'Categories',
}

export function catalogFieldLabel(field: string) {
  return Object.hasOwn(publishedFields, field) ? publishedFields[field] : field
}

export function catalogFieldsSummary(fields: readonly string[]) {
  return fields.map(catalogFieldLabel).join(', ') || 'No fields selected'
}

export function catalogShareState(share: Pick<CatalogShare, 'enabled' | 'expires_at'>) {
  if (share.expires_at && Date.parse(share.expires_at) <= Date.now()) return 'expired'
  return share.enabled ? 'enabled' : 'disabled'
}

// Describe only applied settings: disabled limits are retained in the editor,
// but must not be presented as restrictions on the live connection.
export function connectionSummary(
  share: CatalogShare,
  format: ConnectionFormat | undefined = share.format,
) {
  if (format !== 'stremio' && format !== 'torznab') return undefined
  const stremio = format === 'stremio'
  const value = stremio ? share.stream_preferences : share.torznab_preferences
  const media = stremio ? share.stream_preferences?.types : share.media_types
  const choices = stremio ? stremioMediaTypes : torznabMediaTypes
  const filters = [
    ...(media ?? []).map((type) => choices.find((choice) => choice.value === type)?.label ?? type),
    ...(value?.services ?? []).map((id) =>
      Object.hasOwn(serviceNames, id) ? serviceNames[id] : id,
    ),
  ]
  const language = share.stream_preferences
  if (stremio && language?.mode && language.mode !== 'all') {
    const preferred = resolvedLanguage(language)
    const label = new Intl.DisplayNames(['en'], { type: 'language' }).of(preferred) || preferred
    filters.push(
      label +
        (language.mode === 'preferred'
          ? ''
          : language.mode === 'multi'
            ? ' + MULTI'
            : ' + MULTI + unknown'),
    )
  }
  if (value?.cached_only) filters.push('Cached only')

  const limits: string[] = []
  if (value?.cached_only) {
    if (value.max_cached) limits.push(`Up to ${number(value.max_cached)} cached results`)
  } else {
    if (value?.max_results) limits.push(`Up to ${number(value.max_results)} results`)
    if (stremio && value?.max_cached) limits.push(`Cache target: ${number(value.max_cached)}`)
  }
  return {
    filters: filters.join(' · ') || 'All results',
    sorting: value?.sorting
      ?.map(
        (rule) =>
          `${sortFields.find((field) => field.value === rule.field)?.label ?? rule.field} (${rule.direction === 'asc' ? 'ascending' : 'descending'})`,
      )
      .join(' → '),
    limits: limits.join(' · '),
    response: value?.response_seconds
      ? `${number(value.response_seconds)} ${value.response_seconds === 1 ? 'second' : 'seconds'}`
      : undefined,
  }
}
