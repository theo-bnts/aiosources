import { useCallback } from 'react'
import { useLocation, useSearchParams, type SetURLSearchParams } from 'react-router-dom'
import { isSourceSettingsPath } from './source-navigation.ts'

const viewLabels: Record<string, string> = {
  '/': 'Sources',
  '/providers': 'Source statistics',
  '/providers/settings': 'All sources',
  '/runs': 'Sources history',
  '/torrents': 'Torrents',
  '/search': 'Search',
  '/public/search': 'Search',
  '/remotes': 'Remote catalogs',
  '/remotes/settings': 'Remote catalogs',
  '/remotes/history': 'Remote catalog history',
  '/sharing/settings': 'Shared catalog settings',
  '/sharing/remotes': 'Remote catalogs',
  '/addon/history': 'Stremio history',
  '/metadata/history': 'Metadata history',
  '/metadata/videos/history': 'Video history',
  '/metadata/books/history': 'Book history',
  '/metadata/identification/history': 'Identification history',
  '/debrid/history': 'Debrid history',
}
type ListView = { pathname: string; search: string; returnView?: ListView }
export type BackLink = { to: string; label: string; state?: { returnView: ListView } }

function viewLabel(pathname: string) {
  if (Object.hasOwn(viewLabels, pathname)) return viewLabels[pathname]
  if (/^\/(?:runs|remotes\/history)\/[A-Za-z0-9_-]{1,128}$/.test(pathname))
    return 'Processed records'
  if (isSourceSettingsPath(pathname)) return 'Source settings'
  if (/^\/(?:public\/)?search\/[1-9][0-9]{0,15}$/.test(pathname)) return 'Source releases'
  return undefined
}

function checkedView(value: unknown, depth = 0): ListView | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value) || depth >= 6) return undefined
  const view = value as Partial<ListView>
  if (
    typeof view.pathname !== 'string' ||
    !viewLabel(view.pathname) ||
    typeof view.search !== 'string' ||
    (view.search !== '' && !view.search.startsWith('?')) ||
    view.search.includes('#')
  )
    return undefined
  const parent = checkedView(view.returnView, depth + 1)
  return {
    pathname: view.pathname,
    search: view.search,
    ...(parent ? { returnView: parent } : {}),
  }
}

function viewFromState(state: unknown, depth = 0) {
  return state && typeof state === 'object'
    ? checkedView((state as { returnView?: unknown }).returnView, depth)
    : undefined
}

export function listViewState(view: ListView, state?: unknown) {
  const current = checkedView(view)
  if (!current) return undefined
  const parent = viewFromState(state, 1)
  return { returnView: { ...current, ...(parent ? { returnView: parent } : {}) } }
}

export function detailBackLink(state: unknown, fallback?: BackLink): BackLink | undefined {
  if (!fallback) return fallback
  const view = viewFromState(state)
  if (!view) return fallback
  return {
    to: view.pathname + view.search,
    label: viewLabel(view.pathname)!,
    ...(view.returnView ? { state: { returnView: view.returnView } } : {}),
  }
}

export function useListViewState() {
  const { pathname, search, state } = useLocation()
  return listViewState({ pathname, search }, state)
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
    (next, options) => {
      const focused = options?.flushSync ? document.activeElement : null
      const selection =
        (focused instanceof HTMLInputElement || focused instanceof HTMLTextAreaElement) &&
        focused.selectionStart !== null
          ? ([focused.selectionStart, focused.selectionEnd ?? focused.selectionStart] as const)
          : undefined
      setSearch(next, { state, ...options })
      // Applied query values can replace their fields. Keep keyboard users in
      // the same control without stealing focus from an explicitly opened view.
      if (
        focused instanceof HTMLElement &&
        focused.id &&
        !focused.isConnected &&
        document.activeElement === document.body
      ) {
        const replacement = document.getElementById(focused.id)
        if (replacement?.tagName === focused.tagName) {
          replacement.focus({ preventScroll: true })
          if (
            selection &&
            (replacement instanceof HTMLInputElement || replacement instanceof HTMLTextAreaElement)
          )
            replacement.setSelectionRange(...selection)
        }
      }
    },
    [setSearch, state],
  )
  return [search, update] as const
}
