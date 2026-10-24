import { serviceNames } from '@/lib/debrid'
import { stremioMediaTypes } from '@/components/connection-filter-fields'
import { InstallationFilters } from '@/components/installation-filters'
import { LanguageFilter } from '@/components/language-filter'
import type { StremioPreferences } from '@/lib/catalog-types'
import { defaultLanguagePreferences, resolvedLanguage } from '@/lib/language-preferences'

export function defaultStremioPreferences(): StremioPreferences {
  return defaultLanguagePreferences()
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
        onChange={(language) => onChange({ ...value, ...language })}
      />
    </InstallationFilters>
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
  return filters.join(' · ') || 'All results'
}
