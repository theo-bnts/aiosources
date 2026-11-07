import { useViewSearchParams } from '@/lib/detail-navigation'
import { resetListView } from '@/lib/list-view'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ClearFiltersButton, ResultCount } from '@/components/common'
import { FilterSelect } from '@/components/filter-select'
import { selectionMatches } from '@/lib/filter-values'
import { compareListLabels, matchesListQuery } from '@/lib/list-view'

const directories = {
  source: ['Source filters', 'Find a source', 'sources', 'Name or identifier'],
  remote: [
    'Remote catalog filters',
    'Find a remote catalog',
    'remote catalogs',
    'Name or identifier',
  ],
  access: ['Access filters', 'Find an access key', 'access keys', 'Name or identifier'],
  share: ['Share filters', 'Find a share', 'shares', 'Name or identifier'],
  proxy: ['Proxy filters', 'Find a proxy', 'proxies', 'Name or address'],
  secret: ['Secret filters', 'Find a secret', 'secrets', 'Reference name'],
} as const

export function useListFilters() {
  const [search, setSearch] = useViewSearchParams()
  return {
    query: search.get('q') ?? '',
    state: search.get('state') ?? '',
    selection: search.get('selection') ?? '',
    matchesItem: (id: string, ...names: (string | undefined)[]) =>
      selectionMatches(search.get('selection') ?? '', id) &&
      matchesListQuery(search.get('q') ?? '', ...names),
    matchesState: (...states: string[]) =>
      states.some((state) => selectionMatches(search.get('state') ?? '', state)),
    change(key: 'q' | 'state' | 'selection', value: string) {
      const next = resetListView(search)
      if (key === 'selection') next.delete('q')
      value ? next.set(key, value) : next.delete(key)
      setSearch(next, { replace: true, flushSync: true, preventScrollReset: true })
    },
    clear() {
      setSearch(resetListView(search, ['q', 'state', 'selection']), {
        replace: true,
        flushSync: true,
        preventScrollReset: true,
      })
    },
  }
}

export function ListFilters({
  kind = 'source',
  filters,
  states = [],
  count,
  total,
  entries,
}: {
  kind?: keyof typeof directories
  filters: ReturnType<typeof useListFilters>
  states?: readonly { value: string; label: string }[]
  count: number
  total: number
  entries?: readonly { id: string; name?: string }[]
}) {
  const [label, queryLabel, resultLabel, placeholder] = directories[kind]
  const entityLabel =
    kind === 'source'
      ? 'Source'
      : kind === 'remote'
        ? 'Remote catalog'
        : kind === 'access'
          ? 'Access'
          : kind === 'share'
            ? 'Share'
            : 'Proxy'
  return (
    <section className="space-y-3 rounded-md border border-border p-4" aria-label={label}>
      <div className="flex flex-wrap items-end gap-3">
        <div className="field min-w-48 flex-1 sm:max-w-md">
          <Label htmlFor={`${kind}-query`}>{entries ? entityLabel : queryLabel}</Label>
          {entries ? (
            <FilterSelect
              id={`${kind}-query`}
              label={entityLabel}
              allLabel={`All ${resultLabel}`}
              options={entries
                .map((entry) => ({ value: entry.id, label: entry.name || entry.id }))
                .sort(compareListLabels)}
              value={filters.selection}
              onChange={(value) => filters.change('selection', value)}
            />
          ) : (
            <Input
              id={`${kind}-query`}
              type="search"
              value={filters.query}
              onChange={(event) => filters.change('q', event.target.value)}
              placeholder={placeholder}
            />
          )}
        </div>
        {states.length > 0 && (
          <div className="field min-w-44">
            <Label htmlFor={`${kind}-state`}>Status</Label>
            <FilterSelect
              id={`${kind}-state`}
              label="Status"
              allLabel="All statuses"
              value={filters.state}
              onChange={(value) => filters.change('state', value)}
              options={[...states]}
            />
          </div>
        )}
        <ClearFiltersButton
          active={Boolean(filters.query || filters.selection || (states.length && filters.state))}
          onClear={filters.clear}
        />
      </div>
      {entries && filters.query && (
        <p className="help">
          Matching “{filters.query}”. Choose {entityLabel.toLowerCase()} or clear filters to replace
          this saved text filter.
        </p>
      )}
      <ResultCount total={total} shown={count} label={resultLabel} />
    </section>
  )
}
