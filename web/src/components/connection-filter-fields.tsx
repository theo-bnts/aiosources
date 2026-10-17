import { SourceFilter } from '@/components/source-filter'
import { MultiSelect } from '@/components/ui/multi-select'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export const stremioMediaTypes = [
  { value: 'movie', label: 'Movies' },
  { value: 'series', label: 'Series' },
  { value: 'anime', label: 'Anime' },
]
export const torznabMediaTypes = [
  { value: 'movie', label: 'Movies' },
  { value: 'tv', label: 'Series' },
  { value: 'other', label: 'Other / not identified' },
]
export const debridServices = [
  { id: 'alldebrid', name: 'AllDebrid' },
  { id: 'torbox', name: 'TorBox' },
]

export function MediaTypeFilter({
  id,
  value,
  options,
  onChange,
}: {
  id: string
  value: string[]
  options: { value: string; label: string }[]
  onChange: (value: string[]) => void
}) {
  return (
    <div className="field">
      <Label htmlFor={id}>Media types</Label>
      <MultiSelect
        id={id}
        label="Media types"
        allLabel="All media"
        value={value}
        options={options}
        onChange={onChange}
      />
    </div>
  )
}

export function ConnectionFilterFields({
  id,
  media,
  mediaOptions,
  onMediaChange,
  sources,
  sourceIDs,
  onSourcesChange,
  services,
  serviceIDs,
  onServicesChange,
  cachedOnly,
  onCachedChange,
  showSources = false,
}: {
  id: string
  media: string[]
  mediaOptions: { value: string; label: string }[]
  onMediaChange: (value: string[]) => void
  sources: { id: string; name: string }[]
  sourceIDs: string[]
  onSourcesChange: (value: string[]) => void
  services: { id: string; name: string }[]
  serviceIDs: string[]
  onServicesChange: (value: string[]) => void
  cachedOnly: boolean
  onCachedChange: (value: boolean) => void
  showSources?: boolean
}) {
  return (
    <div className="space-y-4">
      <MediaTypeFilter
        id={`${id}-media`}
        value={media}
        options={mediaOptions}
        onChange={onMediaChange}
      />
      {(showSources || sources.length > 0) && (
        <SourceFilter
          id={`${id}-sources`}
          value={sourceIDs}
          sources={sources}
          onChange={onSourcesChange}
        />
      )}
      <div className="grid gap-4 sm:grid-cols-2 sm:items-end">
        <div className="flex min-h-9 items-center justify-between gap-3">
          <Label htmlFor={`${id}-cached`}>Cached results only</Label>
          <Switch id={`${id}-cached`} checked={cachedOnly} onCheckedChange={onCachedChange} />
        </div>
        {services.length > 0 && (
          <div className="field">
            <Label htmlFor={`${id}-services`}>Services</Label>
            <MultiSelect
              id={`${id}-services`}
              label="Services"
              allLabel="All services"
              value={serviceIDs}
              options={services.map((service) => ({ value: service.id, label: service.name }))}
              onChange={onServicesChange}
            />
          </div>
        )}
      </div>
    </div>
  )
}
