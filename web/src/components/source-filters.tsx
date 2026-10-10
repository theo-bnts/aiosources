import { useViewSearchParams } from '@/lib/detail-navigation'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ClearFiltersButton } from '@/components/common'
import { resetListView } from '@/lib/list-view'

export function useSourceFilters() {
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

export function sourceMatches(query: string, ...values: (string | undefined)[]) {
  const words = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean)
  const text = values.filter(Boolean).join(' ').toLocaleLowerCase()
  return words.every((word) => text.includes(word))
}

export function SourceFilters({
  filters,
  states,
  count,
  total,
}: {
  filters: ReturnType<typeof useSourceFilters>
  states: readonly { value: string; label: string }[]
  count: number
  total: number
}) {
  return (
    <section className="space-y-3" aria-label="Source filters">
      <div className="flex flex-wrap items-end gap-3">
        <div className="field min-w-48 flex-1 sm:max-w-md">
          <Label htmlFor="source-query">Find a source</Label>
          <Input
            id="source-query"
            type="search"
            value={filters.query}
            onChange={(event) => filters.change('q', event.target.value)}
            placeholder="Name or identifier"
          />
        </div>
        <div className="field min-w-44">
          <Label htmlFor="source-state">Status</Label>
          <select
            id="source-state"
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
        <ClearFiltersButton
          active={Boolean(filters.query || filters.state)}
          onClear={filters.clear}
        />
      </div>
      <p className="help" role="status">
        Showing {count} of {total} {total === 1 ? 'source' : 'sources'}
      </p>
    </section>
  )
}
