import { serviceNames } from '@/lib/debrid'
import type { InstallationOptions } from '@/lib/language-preferences'
import { ConnectionFilterFields, stremioMediaTypes } from '@/components/connection-filter-fields'
import { Disclosure } from '@/components/ui/disclosure'
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
      {children}
      {showHelp && (
        <p className="help">
          Filters belong to this link. Copy or install a separate link for each configuration;
          changing these controls does not change existing installations.
        </p>
      )}
    </div>
  )
  return <Disclosure title="Filters">{fields}</Disclosure>
}
