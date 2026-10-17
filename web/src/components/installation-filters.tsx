import type { InstallationOptions } from '@/lib/language-preferences'
import { SourceFilter } from '@/components/source-filter'
import { Disclosure } from '@/components/ui/disclosure'
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
        name: `${{ alldebrid: 'AllDebrid', torbox: 'TorBox', direct: 'Direct torrents' }[id] || id} (not configured)`,
      })),
  ]
  const fields = (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {(showSources || sources.length > 0) && (
          <SourceFilter
            id={`${id}-sources`}
            value={value.sources ?? []}
            sources={sources}
            onChange={(sources) => onChange({ ...value, sources })}
          />
        )}
        <div className="field">
          <Label htmlFor={`${id}-type`}>Media type</Label>
          <select
            className="native-select w-full"
            id={`${id}-type`}
            value={value.types?.[0] ?? ''}
            onChange={(event) =>
              onChange({ ...value, types: event.target.value ? [event.target.value] : [] })
            }
          >
            <option value="">All media</option>
            <option value="movie">Movies</option>
            <option value="series">Series</option>
            <option value="anime">Anime</option>
          </select>
        </div>
        {serviceOptions.length > 1 && (
          <fieldset className="space-y-2">
            <legend className="mb-2 text-sm font-medium">Playback services</legend>
            {serviceOptions.map((service) => (
              <label className="flex items-center gap-2 text-sm" key={service.id}>
                <input
                  type="checkbox"
                  className="accent-primary"
                  checked={!value.services?.length || value.services.includes(service.id)}
                  disabled={value.services?.length === 1 && value.services.includes(service.id)}
                  onChange={(event) => {
                    const current = value.services?.length
                      ? value.services
                      : serviceOptions.map((item) => item.id)
                    const next = event.target.checked
                      ? [...current, service.id]
                      : current.filter((id) => id !== service.id)
                    if (next.length)
                      onChange({
                        ...value,
                        services: next.length === serviceOptions.length ? [] : next,
                      })
                  }}
                />
                <span>{service.name}</span>
              </label>
            ))}
          </fieldset>
        )}
      </div>
      <div className="flex items-center justify-between gap-3">
        <Label htmlFor={`${id}-cached`}>Cached results only</Label>
        <Switch
          id={`${id}-cached`}
          checked={value.cached_only ?? false}
          onCheckedChange={(cached_only) => onChange({ ...value, cached_only })}
        />
      </div>
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
