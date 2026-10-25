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
