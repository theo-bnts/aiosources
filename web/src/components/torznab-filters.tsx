import type { TorznabOptions } from '@/lib/torznab-preferences'
import {
  ConnectionFilterFields,
  torznabMediaTypes,
  debridServices,
} from '@/components/connection-filter-fields'

export type { TorznabOptions } from '@/lib/torznab-preferences'
export function TorznabFilters({
  value,
  onChange,
  sources,
  mediaTypes,
  showSources = false,
  id = 'torznab',
}: {
  value: TorznabOptions
  onChange: (value: TorznabOptions) => void
  sources: { id: string; name: string }[]
  id?: string
  mediaTypes?: string[]
  showSources?: boolean
}) {
  return (
    <ConnectionFilterFields
      id={id}
      media={value.media ?? []}
      mediaOptions={torznabMediaTypes.filter(
        (type) => !mediaTypes?.length || mediaTypes.includes(type.value),
      )}
      onMediaChange={(media) => onChange({ ...value, media })}
      sources={sources}
      sourceIDs={value.sources ?? []}
      onSourcesChange={(sources) => onChange({ ...value, sources })}
      services={debridServices}
      serviceIDs={value.services ?? []}
      onServicesChange={(services) => onChange({ ...value, services })}
      cachedOnly={value.cached_only ?? false}
      onCachedChange={(cached_only) => onChange({ ...value, cached_only })}
      showSources={showSources}
    />
  )
}
