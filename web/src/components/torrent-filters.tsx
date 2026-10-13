import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, ApiError, queryClient } from '@/lib/api'
import { useDiscardChanges, useUnsavedChanges } from '@/lib/unsaved-changes'
import type { Items, ProviderSummary } from '@/lib/types'
import {
  torrentFilterKeys,
  torrentSortOptions,
  type SavedView,
  type TorrentFilters,
} from '@/lib/search-types'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SourceMultiSelect } from '@/components/source-multi-select'
import { ClearFiltersButton, ConfirmAction, ErrorNotice, Loading } from '@/components/common'

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
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
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
      aria-label="Torrent search"
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
            const parsed = new Date(next[key])
            if (Number.isNaN(parsed.getTime())) {
              setError(new Error('Enter a valid publication date and time.'))
              setInvalidFields([key])
              return
            }
            next[key] = parsed.toISOString()
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
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_13rem] sm:items-end">
        <div className="field">
          <Label htmlFor="torrent-search">Search text</Label>
          <Input
            id="torrent-search"
            name="q"
            defaultValue={searchText}
            placeholder="Title, ID or info hash (complete or partial)"
          />
        </div>
        <div className="field">
          <Label htmlFor="torrent-sort">Sort results</Label>
          <select
            id="torrent-sort"
            name="sort"
            className="native-select w-full"
            defaultValue={filters.sort ?? 'recent'}
          >
            {filters.sort && !torrentSortOptions.some((item) => item.value === filters.sort) && (
              <option value={filters.sort}>{filters.sort}</option>
            )}
            {torrentSortOptions.map((item) => (
              <option key={item.value} value={item.value}>
                {item.label}
              </option>
            ))}
          </select>
        </div>
      </div>
      <p className="help">Combine fragments, "exact phrases", -exclusions or OR alternatives.</p>
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
          const local = valid
            ? new Date(timestamp.getTime() - timestamp.getTimezoneOffset() * 60000)
                .toISOString()
                .slice(0, -1)
            : ''
          return (
            <div className="field sm:col-span-2" key={key}>
              <Label htmlFor={`torrent-${key}`}>
                {key === 'published_after' ? 'Published after' : 'Published before'} (browser local
                time)
              </Label>
              <Input
                id={`torrent-${key}`}
                name={key}
                type="datetime-local"
                step="0.001"
                aria-invalid={invalidFields.includes(key) || undefined}
                aria-describedby={invalidFields.includes(key) ? 'torrent-filter-error' : undefined}
                defaultValue={local}
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
      <div className="flex flex-wrap gap-2">
        <Button type="submit">Search</Button>
        <ClearFiltersButton
          onClear={() => {
            form.current?.reset()
            setError(null)
            setInvalidFields([])
            setSelectedSources([])
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

export function SavedTorrentViews({
  filters,
  onApply,
}: {
  filters: TorrentFilters
  onApply: (filters: TorrentFilters) => void
}) {
  const views = useQuery({
    queryKey: ['saved-views'],
    queryFn: ({ signal }) => api<Items<SavedView>>('/saved-views', { signal }),
  })
  const [selectedId, setSelectedId] = useState('')
  const [original, setOriginal] = useState<SavedView | null>(null)
  const [name, setName] = useState('')
  const [message, setMessage] = useState('')
  const selected = views.data?.items?.find((view) => view.id === selectedId)
  const save = useMutation({
    mutationFn: (update: boolean) => {
      if (!name.trim()) throw new Error('Enter a name for this saved view.')
      if (update && (!selected || !original))
        throw new Error('Select an existing saved view first.')
      return api<SavedView>(
        update ? `/saved-views/${encodeURIComponent(selected!.id)}` : '/saved-views',
        {
          method: update ? 'PUT' : 'POST',
          body: JSON.stringify({
            name: name.trim(),
            filters,
            ...(update ? { revision: original!.revision } : {}),
          }),
        },
      )
    },
    onMutate: () => queryClient.cancelQueries({ queryKey: ['saved-views'] }),
    onSuccess: async (saved) => {
      await queryClient.cancelQueries({ queryKey: ['saved-views'] })
      queryClient.setQueryData<Items<SavedView>>(['saved-views'], (old) => ({
        items: [...(old?.items ?? []).filter((item) => item.id !== saved.id), saved],
      }))
      setSelectedId(saved.id)
      setOriginal(saved)
      setName(saved.name)
      setMessage('Saved the currently applied search filters.')
      void queryClient.invalidateQueries({ queryKey: ['saved-views'] })
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409)
        void queryClient.invalidateQueries({ queryKey: ['saved-views'] })
    },
  })
  const conflict =
    Boolean(original && selected && original.revision !== selected.revision) ||
    (save.error instanceof ApiError && save.error.status === 409)
  useUnsavedChanges(name !== (original?.name ?? ''), 'saved view', save.isPending, 'saved-view')
  const discard = useDiscardChanges('saved-view')
  return (
    <Disclosure title="Saved views">
      <p className="help">
        Save the applied search, not unsent form changes. Views store filters and sorting, never a
        fixed list of records.
      </p>
      <ErrorNotice error={views.error} retry={() => void views.refetch()} />
      {views.isPending ? (
        <Loading label="Loading saved views" />
      ) : (
        views.data && (
          <>
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-end">
              <div className="field">
                <Label htmlFor="saved-torrent-view">Saved view</Label>
                <select
                  id="saved-torrent-view"
                  className="native-select w-full"
                  value={selectedId}
                  disabled={save.isPending}
                  onChange={(event) => {
                    const id = event.target.value
                    discard(() => {
                      const view = views.data?.items?.find((item) => item.id === id) ?? null
                      setSelectedId(id)
                      setOriginal(view)
                      setName(view?.name ?? '')
                      setMessage('')
                      save.reset()
                    })
                  }}
                >
                  <option value="">New saved view</option>
                  {views.data.items?.map((view) => (
                    <option key={view.id} value={view.id}>
                      {view.name}
                    </option>
                  ))}
                </select>
              </div>
              <Button
                variant="outline"
                disabled={!selected || save.isPending}
                onClick={() => {
                  if (selected) {
                    onApply(selected.filters)
                    setMessage(`Applied “${selected.name}”.`)
                  }
                }}
              >
                Apply view
              </Button>
              <ConfirmAction
                title="Delete this saved view?"
                description={`Delete “${selected?.name ?? ''}”? The records and currently applied filters will not be changed.`}
                label="Delete view"
                destructive
                trigger={
                  <Button variant="ghost" disabled={!selected || save.isPending}>
                    Delete
                  </Button>
                }
                action={async () => {
                  if (!selected) return
                  await api<void>(`/saved-views/${encodeURIComponent(selected.id)}`, {
                    method: 'DELETE',
                    body: JSON.stringify({ revision: selected.revision }),
                  })
                  queryClient.setQueryData<Items<SavedView>>(['saved-views'], (old) => ({
                    items: old?.items?.filter((item) => item.id !== selected.id) ?? [],
                  }))
                  setSelectedId('')
                  setOriginal(null)
                  setName('')
                  setMessage('Saved view deleted. Published records are unchanged.')
                  await queryClient.invalidateQueries({ queryKey: ['saved-views'] })
                }}
              />
            </div>
            {!views.data.items?.length && (
              <p className="help">
                No saved views yet. Apply a search, name it below, and save it.
              </p>
            )}
          </>
        )
      )}
      <form
        className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-end"
        onSubmit={(event) => {
          event.preventDefault()
          setMessage('')
          save.mutate(false)
        }}
      >
        <div className="field">
          <Label htmlFor="saved-view-name">View name</Label>
          <Input
            id="saved-view-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            maxLength={120}
            required
            disabled={save.isPending}
            placeholder="Name this search"
          />
        </div>
        <Button type="submit" variant="outline" disabled={save.isPending || !name.trim()}>
          {save.isPending ? 'Saving…' : 'Save as new'}
        </Button>
        <Button
          type="button"
          disabled={!selected || !name.trim() || save.isPending || conflict}
          onClick={() => {
            setMessage('')
            save.mutate(true)
          }}
        >
          Update selected
        </Button>
      </form>
      <ErrorNotice error={save.error} retry={() => void views.refetch()} />
      {conflict && selected && (
        <div className="notice space-y-3">
          <p>
            This view changed elsewhere. Load the latest version before updating it. Your edited
            name and applied filters will be kept.
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending || views.isFetching}
            onClick={async () => {
              const latest = await views.refetch()
              const view = latest.data?.items?.find((item) => item.id === selectedId)
              if (!view || latest.isError) return
              if (name === original?.name) setName(view.name)
              setOriginal(view)
              save.reset()
            }}
          >
            Load latest view
          </Button>
        </div>
      )}
      {message && (
        <p role="status" className="help">
          {message}
        </p>
      )}
    </Disclosure>
  )
}
