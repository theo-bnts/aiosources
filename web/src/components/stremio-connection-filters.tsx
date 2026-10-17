import { InstallationFilters } from '@/components/installation-filters'
import { LanguageFilter } from '@/components/language-filter'
import type { StremioPreferences } from '@/lib/catalog-types'
import { browserLanguage } from '@/lib/language-preferences'

export function defaultStremioPreferences(): StremioPreferences {
  return { language: browserLanguage(), mode: 'unknown' }
}

export function StremioConnectionFilters({
  value,
  onChange,
  services,
}: {
  value: StremioPreferences
  onChange: (value: StremioPreferences) => void
  services: { id: string; name: string }[]
}) {
  return (
    <InstallationFilters
      value={value}
      onChange={(options) => onChange({ ...value, ...options })}
      sources={[]}
      services={services}
      showHelp={false}
    >
      <LanguageFilter value={value} onChange={(language) => onChange({ ...value, ...language })} />
    </InstallationFilters>
  )
}

export function stremioPreferenceSummary(value?: StremioPreferences) {
  if (!value) return 'All results'
  const filters: string[] = []
  if (value.types?.length)
    filters.push(
      ...value.types.map(
        (type) => ({ movie: 'Movies', series: 'Series', anime: 'Anime' })[type] || type,
      ),
    )
  if (value.services?.length)
    filters.push(
      ...value.services.map(
        (id) => ({ alldebrid: 'AllDebrid', torbox: 'TorBox', direct: 'Direct torrents' })[id] || id,
      ),
    )
  if (value.mode && value.mode !== 'all') {
    const language =
      new Intl.DisplayNames(['en'], { type: 'language' }).of(value.language) || value.language
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
