import { Disclosure } from '@/components/ui/disclosure'
import { QuerySorting } from '@/components/result-controls'
import { DateTimeInput, useDateTimeZoneLabel } from '@/components/date-time-input'
import { dateTimeToISO } from '@/lib/format'
import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Items, ProviderSummary } from '@/lib/types'
import { torrentFilterKeys, torrentSorting, type TorrentFilters } from '@/lib/search-types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SourceMultiSelect } from '@/components/source-multi-select'
import { ClearFiltersButton, ErrorNotice } from '@/components/common'

const rangeFields = [
  ['min_size', 'Minimum size (bytes)', '0'],
  ['max_size', 'Maximum size (bytes)', 'No maximum'],
] as const

export function TorrentSearchForm({
  filters,
  onApply,
}: {
  filters: TorrentFilters
  onApply: (filters: TorrentFilters) => void
}) {
  const dateZone = useDateTimeZoneLabel()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const [sort, setSort] = useState<string>(filters.sort || 'recent')
  const [error, setError] = useState<Error | null>(null)
  const [invalidFields, setInvalidFields] = useState<string[]>([])
  const form = useRef<HTMLFormElement>(null)
  const dateInputs = useRef<Record<string, { input: HTMLInputElement; value: string }>>({})
  const choices = new Map((filters.providers ?? []).map((id) => [id, id]))
  for (const provider of providers.data?.items ?? [])
    choices.set(provider.id, provider.name || provider.id)
  const [selectedSources, setSelectedSources] = useState(filters.providers ?? [])
  const searchText = [filters.q, filters.info_hash].filter(Boolean).join(' ')
  return (
    <form
      ref={form}
      className="space-y-4 rounded-md border border-border p-4"
      aria-label="Torrents"
      onSubmit={(event) => {
        event.preventDefault()
        setError(null)
        setInvalidFields([])
        const data = new FormData(event.currentTarget)
        const next: TorrentFilters = {}
        for (const key of torrentFilterKeys) {
          const value = String(data.get(key) ?? '').trim()
          if (value) Object.assign(next, { [key]: value })
        }
        next.providers = data.getAll('provider').map(String)
        // Retain an unchanged legacy exact-hash constraint, including its AND
        // relationship with OR queries. Editing the merged text starts a new query.
        if (filters.info_hash && next.q === searchText) {
          next.q = filters.q
          next.info_hash = filters.info_hash
        }
        for (const key of ['published_after', 'published_before'] as const) {
          const initial = dateInputs.current[key]
          // Compare the browser's actual initial value, which may normalize or
          // truncate the displayed timestamp, not the original RFC3339 string.
          if (
            filters[key] &&
            initial &&
            initial.input.value === initial.value &&
            !Number.isNaN(new Date(filters[key]).getTime())
          ) {
            next[key] = filters[key]
            continue
          }
          if (next[key]) {
            const instant = dateTimeToISO(next[key]!, filters[key], 'milliseconds')!
            if (Number.isNaN(new Date(instant).getTime())) {
              setError(new Error('Enter a valid publication date and time.'))
              setInvalidFields([key])
              return
            }
            next[key] = instant
          }
        }
        const invalidSize = (['min_size', 'max_size'] as const).find(
          (key) => next[key] && !/^[0-9]{1,1000}$/.test(next[key]!),
        )
        if (invalidSize) {
          setError(
            new Error('Enter a nonnegative whole size without separators (up to 1,000 digits).'),
          )
          setInvalidFields([invalidSize])
          return
        }
        // Byte counts can exceed JavaScript's safe integer range. Compare exact
        // integers, as the server does, before replacing the applied results.
        if (next.min_size && next.max_size && BigInt(next.min_size) > BigInt(next.max_size)) {
          setError(new Error('Minimum size must not exceed maximum size.'))
          setInvalidFields(['min_size', 'max_size'])
          return
        }
        if (
          next.published_after &&
          next.published_before &&
          new Date(next.published_after) > new Date(next.published_before)
        ) {
          setError(new Error('Published after must not be later than published before.'))
          setInvalidFields(['published_after', 'published_before'])
          return
        }
        onApply(next)
      }}
    >
      <div>
        <div className="field">
          <Label htmlFor="torrent-search">Search text</Label>
          <Input
            id="torrent-search"
            name="q"
            defaultValue={searchText}
            placeholder="Title, ID or info hash (complete or partial)"
          />
        </div>
      </div>
      <p className="help">Combine fragments, "exact phrases", -exclusions or OR alternatives.</p>
      <Disclosure title="Filters" open>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <div className="field sm:col-span-2">
            <Label htmlFor="torrent-sources">Sources</Label>
            <SourceMultiSelect
              id="torrent-sources"
              value={selectedSources}
              options={[...choices].map(([value, label]) => ({ value, label }))}
              onChange={setSelectedSources}
            />
          </div>
          <div className="field sm:col-span-2">
            <Label htmlFor="torrent-category">Category</Label>
            <Input
              id="torrent-category"
              name="category"
              defaultValue={filters.category ?? ''}
              placeholder="Exact category, ignoring case"
            />
          </div>
          {rangeFields.map(([key, label, placeholder]) => (
            <div className="field xl:col-span-2" key={key}>
              <Label htmlFor={`torrent-${key}`}>{label}</Label>
              <Input
                id={`torrent-${key}`}
                name={key}
                defaultValue={filters[key] ?? ''}
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={1000}
                aria-invalid={invalidFields.includes(key) || undefined}
                aria-describedby={invalidFields.includes(key) ? 'torrent-filter-error' : undefined}
                title="Enter a nonnegative whole number without separators."
                placeholder={placeholder}
              />
            </div>
          ))}
          {(['published_after', 'published_before'] as const).map((key) => {
            const value = filters[key]
            const timestamp = value ? new Date(value) : undefined
            const valid = timestamp && !Number.isNaN(timestamp.getTime())
            return (
              <div className="field sm:col-span-2" key={key}>
                <Label htmlFor={`torrent-${key}`}>
                  {key === 'published_after' ? 'Published after' : 'Published before'} ({dateZone})
                </Label>
                <DateTimeInput
                  id={`torrent-${key}`}
                  name={key}
                  precision="milliseconds"
                  aria-invalid={invalidFields.includes(key) || undefined}
                  aria-describedby={
                    invalidFields.includes(key) ? 'torrent-filter-error' : undefined
                  }
                  defaultValue={value}
                  ref={(input) => {
                    if (input && dateInputs.current[key]?.input !== input) {
                      dateInputs.current[key] = { input, value: input.value }
                    }
                  }}
                />
                {value && !valid && (
                  <p className="text-xs text-destructive">
                    Invalid date in URL. Choose a valid date or clear filters.
                  </p>
                )}
              </div>
            )
          })}
        </div>
      </Disclosure>
      <QuerySorting value={sort} onChange={setSort} options={torrentSorting} />
      <input type="hidden" name="sort" value={sort} />
      <div className="flex flex-wrap gap-2">
        <Button id="torrent-submit" type="submit">
          Search
        </Button>
        <ClearFiltersButton
          onClear={() => {
            form.current?.reset()
            setError(null)
            setInvalidFields([])
            setSelectedSources([])
            setSort('recent')
            onApply({})
          }}
        />
      </div>
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      {error && (
        <div id="torrent-filter-error">
          <ErrorNotice error={error} />
        </div>
      )}
    </form>
  )
}
