import { useBrowserPreferences } from './browser-preferences.ts'
import { useViewSearchParams } from './detail-navigation'
import { resetListView } from './list-view'

export const pageSize = 50

export function normalizePageOffset(value: string | number | null, limit = pageSize) {
  const offset = Number(value)
  return Number.isSafeInteger(offset) && offset > 0 ? Math.floor(offset / limit) * limit : 0
}

export function useURLPagination({
  limitKey = 'limit',
  offsetKey = 'offset',
}: { limitKey?: string; offsetKey?: string } = {}) {
  const { pageSize: preferredSize } = useBrowserPreferences()
  const [search, setSearch] = useViewSearchParams()
  return {
    limit: preferredSize,
    offset: normalizePageOffset(search.get(offsetKey), preferredSize),
    setOffset(value: number) {
      const next = resetListView(search, [], offsetKey)
      next.delete(limitKey)
      const offset = normalizePageOffset(value, preferredSize)
      if (offset) next.set(offsetKey, String(offset))
      setSearch(next)
    },
  }
}
