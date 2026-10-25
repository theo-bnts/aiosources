import { useViewSearchParams } from '@/lib/detail-navigation'
import { resetListView } from '@/lib/list-view'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ClearFiltersButton, ResultCount } from '@/components/common'

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
    change(key: 'q' | 'state', value: string) {
      const next = resetListView(search)
      value ? next.set(key, value) : next.delete(key)
      setSearch(next, { replace: true })
    },
    clear() {
      setSearch(resetListView(search, ['q', 'state']), { replace: true })
    },
  }
}

export function ListFilters({
  kind = 'source',
  filters,
  states = [],
  count,
  total,
}: {
  kind?: keyof typeof directories
  filters: ReturnType<typeof useListFilters>
  states?: readonly { value: string; label: string }[]
  count: number
  total: number
}) {
  const [label, queryLabel, resultLabel, placeholder] = directories[kind]
  return (
    <section className="space-y-3 rounded-md border border-border p-4" aria-label={label}>
      <div className="flex flex-wrap items-end gap-3">
        <div className="field min-w-48 flex-1 sm:max-w-md">
          <Label htmlFor={`${kind}-query`}>{queryLabel}</Label>
          <Input
            id={`${kind}-query`}
            type="search"
            value={filters.query}
            onChange={(event) => filters.change('q', event.target.value)}
            placeholder={placeholder}
          />
        </div>
        {states.length > 0 && (
          <div className="field min-w-44">
            <Label htmlFor={`${kind}-state`}>Status</Label>
            <select
              id={`${kind}-state`}
              className="native-select"
              value={filters.state}
              onChange={(event) => filters.change('state', event.target.value)}
            >
              <option value="">All statuses</option>
              {filters.state && !states.some((state) => state.value === filters.state) && (
                <option value={filters.state}>Unknown status</option>
              )}
              {states.map((state) => (
                <option key={state.value} value={state.value}>
                  {state.label}
                </option>
              ))}
            </select>
          </div>
        )}
        <ClearFiltersButton
          active={Boolean(filters.query || (states.length && filters.state))}
          onClear={filters.clear}
        />
      </div>
      <ResultCount total={total} shown={count} label={resultLabel} />
    </section>
  )
}
