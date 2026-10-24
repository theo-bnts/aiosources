import { serviceNames } from '@/lib/debrid'
import { stremioMediaTypes } from '@/components/connection-filter-fields'
import { InstallationFilters } from '@/components/installation-filters'
import { LanguageFilter } from '@/components/language-filter'
import type { StremioPreferences } from '@/lib/catalog-types'
import { defaultLanguagePreferences, resolvedLanguage } from '@/lib/language-preferences'
import { ResultSorting } from '@/components/result-controls'
import { resultPreferences } from '@/lib/result-preferences'

export function defaultStremioPreferences(): StremioPreferences {
  return defaultLanguagePreferences()
}

export function stremioPreferencesKey(value: StremioPreferences) {
  return JSON.stringify({
    language: resolvedLanguage(value),
    mode: value.mode || 'all',
    ...(value.services?.length ? { services: value.services } : {}),
    ...(value.types?.length ? { types: value.types } : {}),
    ...(value.cached_only ? { cached_only: true } : {}),
    ...resultPreferences(value),
  })
}

export function StremioConnectionFilters({
  value,
  onChange,
  services,
  sources,
  sourceIDs,
  onSourceIDsChange,
}: {
  value: StremioPreferences
  onChange: (value: StremioPreferences) => void
  services: { id: string; name: string }[]
  sources: { id: string; name: string }[]
  sourceIDs: string[]
  onSourceIDsChange: (value: string[]) => void
}) {
  return (
    <>
      <InstallationFilters
        value={{ ...value, sources: sourceIDs }}
        onChange={({ sources: selected, ...options }) => {
          onSourceIDsChange(selected ?? [])
          onChange({ ...value, ...options })
        }}
        sources={sources}
        services={services}
        showHelp={false}
        showSources
      >
        <LanguageFilter
          layout="stacked"
          value={value}
          sorting={value.sorting?.some((rule) => rule.field === 'language')}
          onChange={(language) => onChange({ ...value, ...language })}
        />
      </InstallationFilters>
      <ResultSorting
        value={value.sorting}
        onChange={(sorting) => onChange({ ...value, sorting })}
        language
      />
    </>
  )
}

export function stremioPreferenceSummary(value?: StremioPreferences) {
  if (!value) return 'All results'
  const filters: string[] = []
  if (value.types?.length)
    filters.push(
      ...value.types.map(
        (type) => stremioMediaTypes.find((option) => option.value === type)?.label || type,
      ),
    )
  if (value.services?.length) filters.push(...value.services.map((id) => serviceNames[id] || id))
  if (value.mode && value.mode !== 'all') {
    const preferred = resolvedLanguage(value)
    const language = new Intl.DisplayNames(['en'], { type: 'language' }).of(preferred) || preferred
    filters.push(
      language +
        (value.mode === 'preferred'
          ? ''
          : value.mode === 'multi'
            ? ' + MULTI'
            : ' + MULTI + unknown'),
    )
  }
  if (value.cached_only) filters.push('Cached only')
  const limit = value.cached_only ? value.max_cached : value.max_results
  if (limit) filters.push(`Up to ${limit} results`)
  if (!value.cached_only && value.max_cached) filters.push(`Cache target: ${value.max_cached}`)
  if (value.sorting?.length)
    filters.push(`${value.sorting.length} sorting ${value.sorting.length === 1 ? 'rule' : 'rules'}`)
  if (value.response_seconds) filters.push(`${value.response_seconds}s availability wait`)
  return filters.join(' · ') || 'All results'
}
