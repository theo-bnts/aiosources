import { useState } from 'react'
import type { CatalogShare } from '@/lib/catalog-types'
import { SourceMultiSelect } from '@/components/source-multi-select'
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
  const [selected, setSelected] = useState<string[]>([])
  const [category, setCategory] = useState('')
  const url = share.url ? new URL(share.url) : null
  const permitted = sources.filter(
    (source) => share.scope === 'all' || share.source_ids.includes(source.id),
  )
  const filters = new URLSearchParams()
  if (selected.length) filters.set('sources', selected.join(','))
  if (category) filters.set('media', category)
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
          <div className="field">
            <Label htmlFor={`torznab-sources-${share.id}`}>Sources</Label>
            <SourceMultiSelect
              id={`torznab-sources-${share.id}`}
              value={selected}
              options={permitted.map((source) => ({ value: source.id, label: source.name }))}
              onChange={setSelected}
            />
          </div>
          <div className="field">
            <Label htmlFor={`torznab-category-${share.id}`}>Media type</Label>
            <select
              id={`torznab-category-${share.id}`}
              className="native-select w-full"
              value={category}
              onChange={(event) => setCategory(event.target.value)}
            >
              <option value="">All allowed media</option>
              {[
                { type: 'movie', id: 'movie', name: 'Movies' },
                { type: 'tv', id: 'tv', name: 'TV' },
                { type: 'other', id: 'other', name: 'Other' },
              ]
                .filter(
                  (type) => !share.media_types?.length || share.media_types.includes(type.type),
                )
                .map((type) => (
                  <option key={type.id} value={type.id}>
                    {type.name}
                  </option>
                ))}
            </select>
          </div>
        </div>
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
