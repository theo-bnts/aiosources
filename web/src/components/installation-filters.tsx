import type { InstallationOptions } from '@/lib/language-preferences'
import { SourceMultiSelect } from '@/components/source-multi-select'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export function InstallationFilters({
  value,
  onChange,
  sources,
  services,
}: {
  value: InstallationOptions
  onChange: (value: InstallationOptions) => void
  sources: { id: string; name: string }[]
  services: { id: string; name: string }[]
}) {
  return (
    <Disclosure title="Connection filters">
      <div className="grid gap-4 sm:grid-cols-2">
        {sources.length > 0 && (
          <div className="field sm:col-span-2">
            <Label htmlFor="installation-sources">Sources</Label>
            <SourceMultiSelect
              id="installation-sources"
              value={value.sources ?? []}
              options={sources.map((source) => ({ value: source.id, label: source.name }))}
              onChange={(sources) => onChange({ ...value, sources })}
            />
          </div>
        )}
        <div className="field">
          <Label htmlFor="installation-type">Media type</Label>
          <select
            className="native-select w-full"
            id="installation-type"
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
        {services.length > 1 && (
          <fieldset className="space-y-2">
            <legend className="mb-2 text-sm font-medium">Debrid services</legend>
            {services.map((service) => (
              <label className="flex items-center gap-2 text-sm" key={service.id}>
                <input
                  type="checkbox"
                  className="accent-primary"
                  checked={!value.services?.length || value.services.includes(service.id)}
                  disabled={value.services?.length === 1 && value.services.includes(service.id)}
                  onChange={(event) => {
                    const current = value.services?.length
                      ? value.services
                      : services.map((item) => item.id)
                    const next = event.target.checked
                      ? [...current, service.id]
                      : current.filter((id) => id !== service.id)
                    if (next.length)
                      onChange({ ...value, services: next.length === services.length ? [] : next })
                  }}
                />
                <span>{service.name}</span>
              </label>
            ))}
          </fieldset>
        )}
      </div>
      <div className="flex items-center justify-between gap-3">
        <Label htmlFor="installation-cached">Cached results only</Label>
        <Switch
          id="installation-cached"
          checked={value.cached_only ?? false}
          onCheckedChange={(cached_only) => onChange({ ...value, cached_only })}
        />
      </div>
      <p className="help">
        Filters belong to this link. Copy or install a separate link for each configuration;
        changing these controls does not change existing installations.
      </p>
    </Disclosure>
  )
}
