import { useCallback } from 'react'
import { useLocation, useSearchParams, type SetURLSearchParams } from 'react-router-dom'

const viewLabels: Record<string, string> = {
  '/': 'Sources',
  '/providers': 'Source statistics',
  '/providers/settings': 'All sources',
  '/runs': 'Sources history',
  '/torrents': 'Search',
  '/sharing/remotes': 'Remote catalogs',
  '/addon/history': 'Stremio history',
  '/metadata/history': 'Metadata history',
  '/debrid/history': 'Debrid history',
}
type BackLink = { to: string; label: string }
type ListView = { pathname: string; search: string }

function viewLabel(pathname: string) {
  if (Object.hasOwn(viewLabels, pathname)) return viewLabels[pathname]
  if (/^\/runs\/[A-Za-z0-9_-]{1,128}$/.test(pathname)) return 'Processed records'
  if (/^\/providers\/[A-Za-z0-9_-]{1,128}$/.test(pathname)) return 'Source settings'
  return undefined
}

export function listViewState(view: ListView) {
  return viewLabel(view.pathname) ? { returnView: view } : undefined
}

export function detailBackLink(state: unknown, fallback?: BackLink): BackLink | undefined {
  if (!fallback || !state || typeof state !== 'object') return fallback
  const view = (state as { returnView?: Partial<ListView> }).returnView
  if (
    !view ||
    typeof view.pathname !== 'string' ||
    !viewLabel(view.pathname) ||
    typeof view.search !== 'string' ||
    (view.search !== '' && !view.search.startsWith('?')) ||
    view.search.includes('#')
  )
    return fallback
  return { to: view.pathname + view.search, label: viewLabel(view.pathname)! }
}

export function useListViewState() {
  const { pathname, search } = useLocation()
  return listViewState({ pathname, search })
}

export function useDetailBackLink(fallback?: BackLink) {
  return detailBackLink(useLocation().state, fallback)
}

// Updating a detail tab or its filters must keep the originating list view.
// The state contains only the list's existing path and public search parameters.
export function useViewSearchParams() {
  const [search, setSearch] = useSearchParams()
  const { state } = useLocation()
  const update = useCallback<SetURLSearchParams>(
    (next, options) => setSearch(next, { state, ...options }),
    [setSearch, state],
  )
  return [search, update] as const
}
