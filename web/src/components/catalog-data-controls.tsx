import type { ReactNode } from 'react'
import type { CatalogEntities } from '@/lib/catalog-types'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

const torrentOptions = [
  [
    'identifiers',
    'Identifiers and episodes',
    'Media identifiers, categories, years, seasons and episodes. Your instance checks identification locally.',
  ],
  [
    'languages',
    'Audio and subtitle languages',
    'Language labels only. Subtitle links are excluded.',
  ],
  [
    'technical',
    'Audio and video information',
    'Quality, codecs, HDR, audio channels, editions, duration and bitrate.',
  ],
  [
    'files',
    'File information',
    'Source file names, relative folders, sizes and torrent file positions. Download links and provider identifiers are excluded.',
  ],
] as const

export function CatalogDataControls({
  id,
  value,
  onChange,
  receiving = false,
  allowAvailability = true,
  publishedFields,
}: {
  id: string
  value: CatalogEntities
  onChange: (value: CatalogEntities) => void
  receiving?: boolean
  allowAvailability?: boolean
  publishedFields?: ReactNode
}) {
  const control = (
    key: keyof CatalogEntities,
    label: string,
    description: string,
    disabled = false,
  ) => (
    <div key={key} className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <Label htmlFor={`${id}-${key}`}>{label}</Label>
        <p className="help mt-1">{description}</p>
        {disabled && <p className="help mt-1">Include Info hash in Published fields first.</p>}
      </div>
      <Switch
        id={`${id}-${key}`}
        checked={value[key] ?? false}
        disabled={disabled}
        onCheckedChange={(checked) => onChange({ ...value, [key]: checked })}
      />
    </div>
  )
  return (
    <Disclosure title="Shared data">
      <Disclosure title="Torrents">
        {publishedFields}
        {receiving && (
          <p className="help">The owner chooses which basic torrent fields are available.</p>
        )}
        {torrentOptions.map(([key, label, description]) => control(key, label, description))}
        <p className="help">
          {receiving
            ? 'Changing these choices refreshes the local torrent copy at the next sync. It does not contact the original sources.'
            : 'Only selected source facts are included. Keys, announce URLs, source definitions and raw payloads are excluded.'}
        </p>
      </Disclosure>
      <Disclosure title="Metadata">
        {control(
          'metadata',
          'Film and series information',
          'Titles, translations, identifiers and season information. Duplicates use the same TMDB identity; identification and reviews stay local.',
        )}
      </Disclosure>
      <Disclosure title="Debrid">
        {control(
          'availability',
          'Debrid cache observations',
          'Recent, shareable cache checks and file information. Personal libraries are excluded. Observations expire normally and playback still uses your own account.',
          !allowAvailability,
        )}
      </Disclosure>
      {receiving && (
        <p className="help">
          Choices apply when the owner permits them. Local metadata and fresh local cache checks
          take priority. Received metadata and cache observations are not forwarded.
        </p>
      )}
    </Disclosure>
  )
}

export function catalogEntitiesSummary(value?: CatalogEntities) {
  const details = torrentOptions.filter(([key]) => value?.[key]).length
  return [
    details ? 'Torrents + details' : 'Torrents',
    value?.metadata && 'Metadata',
    value?.availability && 'Debrid cache',
  ]
    .filter(Boolean)
    .join(' · ')
}
