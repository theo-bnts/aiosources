import type { CatalogEntities } from '@/lib/catalog-types'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export function CatalogDataControls({
  id,
  value,
  onChange,
  receiving = false,
  allowAvailability = true,
}: {
  id: string
  value: CatalogEntities
  onChange: (value: CatalogEntities) => void
  receiving?: boolean
  allowAvailability?: boolean
}) {
  return (
    <Disclosure title="Additional data">
      <p className="help">
        {receiving
          ? 'Optional data from a trusted instance, when its owner allows it. Existing local metadata and fresh local cache checks take priority.'
          : 'Optional data linked to the selected sources. Accounts, keys, announce URLs and download links are never included.'}
      </p>
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Label htmlFor={`${id}-metadata`}>Film and series information</Label>
          <p className="help mt-1">
            Titles, translations, identifiers and season information. Duplicates use the same TMDB
            identity; identification and reviews stay local.
          </p>
        </div>
        <Switch
          id={`${id}-metadata`}
          checked={value.metadata ?? false}
          onCheckedChange={(metadata) => onChange({ ...value, metadata })}
        />
      </div>
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <Label htmlFor={`${id}-availability`}>Debrid cache observations</Label>
          <p className="help mt-1">
            Recent, shareable cache checks and file information. Personal libraries are excluded.
            Observations expire normally and playback still uses your own account.
          </p>
          {!allowAvailability && (
            <p className="help mt-1">Select info_hash in Safe fields to share first.</p>
          )}
        </div>
        <Switch
          id={`${id}-availability`}
          checked={value.availability ?? false}
          disabled={!allowAvailability}
          onCheckedChange={(availability) => onChange({ ...value, availability })}
        />
      </div>
      {receiving && (
        <p className="help">
          Changes apply to the next new sync. Received metadata and cache observations are not
          forwarded to other instances.
        </p>
      )}
    </Disclosure>
  )
}

export function catalogEntitiesSummary(value?: CatalogEntities) {
  return ['Torrents', value?.metadata && 'Metadata', value?.availability && 'Debrid cache']
    .filter(Boolean)
    .join(' · ')
}
