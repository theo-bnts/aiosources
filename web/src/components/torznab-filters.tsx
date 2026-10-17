import type { TorznabOptions } from '@/lib/torznab-preferences'
import { SourceFilter } from '@/components/source-filter'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export type { TorznabOptions } from '@/lib/torznab-preferences'
export function TorznabFilters({
  value,
  onChange,
  sources,
  mediaTypes,
  id = 'torznab',
}: {
  value: TorznabOptions
  onChange: (value: TorznabOptions) => void
  sources: { id: string; name: string }[]
  id?: string
  mediaTypes?: string[]
}) {
  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        {sources.length > 0 && (
          <SourceFilter
            id={`${id}-sources`}
            value={value.sources ?? []}
            sources={sources}
            onChange={(sources) => onChange({ ...value, sources })}
          />
        )}
        <div className="field">
          <Label htmlFor={`${id}-media`}>Media type</Label>
          <select
            id={`${id}-media`}
            className="native-select w-full"
            value={value.media?.[0] ?? ''}
            onChange={(e) => onChange({ ...value, media: e.target.value ? [e.target.value] : [] })}
          >
            <option value="">All allowed media</option>
            {[
              { id: 'movie', label: 'Movies' },
              { id: 'tv', label: 'TV' },
              { id: 'other', label: 'Other' },
            ]
              .filter((m) => !mediaTypes?.length || mediaTypes.includes(m.id))
              .map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label}
                </option>
              ))}
          </select>
        </div>
        <div className="field">
          <Label htmlFor={`${id}-service`}>Cache service</Label>
          <select
            id={`${id}-service`}
            className="native-select w-full"
            value={value.services?.[0] ?? ''}
            onChange={(e) =>
              onChange({ ...value, services: e.target.value ? [e.target.value] : [] })
            }
          >
            <option value="">All services</option>
            <option value="alldebrid">AllDebrid</option>
            <option value="torbox">TorBox</option>
          </select>
        </div>
      </div>
      <div className="flex items-center justify-between gap-3">
        <Label htmlFor={`${id}-cached`}>Cached results only</Label>
        <Switch
          id={`${id}-cached`}
          checked={value.cached_only ?? false}
          onCheckedChange={(cached_only) => onChange({ ...value, cached_only })}
        />
      </div>
    </div>
  )
}
