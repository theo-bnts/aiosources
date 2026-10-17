import { useState } from 'react'
import type { CatalogShare } from '@/lib/catalog-types'
import { TorznabFilters, type TorznabOptions } from '@/components/torznab-filters'
import { CopyButton } from '@/components/copy-button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'

export function TorznabSetup({
  share,
  sources,
}: {
  share: CatalogShare
  sources: { id: string; name: string }[]
}) {
  const [value, setValue] = useState<TorznabOptions>({})
  const selected = value.sources ?? []

  const url = share.url ? new URL(share.url) : null
  const permitted = sources.filter(
    (source) => share.scope === 'all' || share.source_ids.includes(source.id),
  )
  const filters = new URLSearchParams()
  if (selected.length) filters.set('sources', selected.join(','))
  if (value.media?.length) filters.set('media', value.media.join(','))
  if (value.services?.length) filters.set('services', value.services.join(','))
  if (value.cached_only) filters.set('cached_only', 'true')
  const additional = filters.size ? '&' + filters.toString() : ''
  // Leave room for the client's ordinary query parameters and API key.
  const tooLong = selected.length > 100 || additional.length > 6000
  return (
    <Disclosure title="Client setup & filters">
      <div className="space-y-4">
        <p className="help">
          In Prowlarr, add Generic Torznab, use this address and API path, and enter the generated
          API key.
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="field">
            <Label htmlFor={`torznab-host-${share.id}`}>URL</Label>
            <Input id={`torznab-host-${share.id}`} readOnly value={url?.origin ?? ''} />
            <CopyButton value={url?.origin ?? ''} label="Copy URL" />
          </div>
          <div className="field">
            <Label htmlFor={`torznab-path-${share.id}`}>API path</Label>
            <Input id={`torznab-path-${share.id}`} readOnly value={url?.pathname ?? ''} />
            <CopyButton value={url?.pathname ?? ''} label="Copy API path" />
          </div>
        </div>
        <Disclosure title="Filters">
          <TorznabFilters
            value={value}
            onChange={setValue}
            sources={permitted}
            mediaTypes={share.media_types}
            id={`torznab-${share.id}`}
          />
        </Disclosure>
        <div className="field">
          <Label htmlFor={`torznab-parameters-${share.id}`}>Additional parameters (optional)</Label>
          <Input
            id={`torznab-parameters-${share.id}`}
            readOnly
            value={additional}
            placeholder="No extra filters"
            className="font-mono"
            aria-invalid={tooLong}
          />
          {tooLong && (
            <p className="text-sm text-destructive" role="alert">
              Select fewer sources or use All sources to keep this link within the request limits.
            </p>
          )}
          <CopyButton value={tooLong ? '' : additional} label="Copy parameters" />
        </div>
        <p className="help">
          Source filters can be combined. These parameters only narrow the permissions above. Copy
          them into your client’s Additional parameters field.
        </p>
      </div>
    </Disclosure>
  )
}
