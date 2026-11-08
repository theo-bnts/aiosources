import { serviceNames } from '@/lib/debrid'
import type { InstallationOptions } from '@/lib/language-preferences'
import { ConnectionFilterFields, stremioMediaTypes } from '@/components/connection-filter-fields'
import { Disclosure } from '@/components/ui/disclosure'
import { ResultLimits } from '@/components/result-controls'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useId, useRef, type ReactNode } from 'react'

export function InstallationFilters({
  value,
  onChange,
  sources,
  services,
  children,
  showHelp = true,
  showSources = false,
}: {
  value: InstallationOptions
  onChange: (value: InstallationOptions) => void
  sources: { id: string; name: string }[]
  services: { id: string; name: string }[]
  children?: ReactNode
  showHelp?: boolean
  showSources?: boolean
}) {
  const id = useId()
  const unavailableServices = useRef(new Set<string>())
  for (const service of value.services ?? []) {
    if (!services.some((option) => option.id === service)) unavailableServices.current.add(service)
  }
  const serviceOptions = [
    ...services,
    ...[...unavailableServices.current]
      .filter((id) => !services.some((service) => service.id === id))
      .map((id) => ({
        id,
        name: `${serviceNames[id] || id} (not configured)`,
      })),
  ]
  const fields = (
    <div className="space-y-4">
      <ConnectionFilterFields
        id={id}
        media={value.types ?? []}
        mediaOptions={stremioMediaTypes}
        onMediaChange={(types) => onChange({ ...value, types })}
        sources={sources}
        sourceIDs={value.sources ?? []}
        onSourcesChange={(sources) => onChange({ ...value, sources })}
        services={serviceOptions}
        serviceIDs={value.services ?? []}
        onServicesChange={(services) => onChange({ ...value, services })}
        cachedOnly={value.cached_only ?? false}
        onCachedChange={(cached_only) => onChange({ ...value, cached_only })}
        showSources={showSources}
      />
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Label
            htmlFor={`${id}-saved-cache`}
            help={
              <>
                Listing results does not contact debrid services. Missing or expired checks stay
                Unknown. Playback still contacts your selected service.
              </>
            }
          >
            Use saved cache checks only
          </Label>
        </div>
        <Switch
          id={`${id}-saved-cache`}
          checked={value.skip_cache_check ?? false}
          onCheckedChange={(skip_cache_check) => onChange({ ...value, skip_cache_check })}
        />
      </div>
      {children}
      <ResultLimits value={value} onChange={onChange} cache localOnly={value.skip_cache_check} />
    </div>
  )
  return (
    <Disclosure
      title="Filters"
      help={
        showHelp
          ? 'Filters belong to this link. Copy or install a separate link for each configuration; changing these controls does not change existing installations.'
          : undefined
      }
    >
      {fields}
    </Disclosure>
  )
}
