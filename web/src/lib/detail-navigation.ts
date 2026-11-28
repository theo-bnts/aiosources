import { useCallback } from 'react'
import { useLocation, useSearchParams, type SetURLSearchParams } from 'react-router-dom'
import { routes } from './routes.ts'
import { filesExplorerPath, isSourceSettingsPath } from './source-navigation.ts'
import {
  identificationCorrespondencesPath,
  identificationResultsPath,
} from './metadata-navigation.ts'

const viewLabels: Record<string, string> = {
  '/': 'Sources',
  [routes.sources.indexers.torrents.statistics]: 'Torrent statistics',
  [routes.sources.indexers.torrents.settings]: 'All sources',
  [routes.sources.indexers.torrents.history]: 'Torrent history',
  [routes.sources.files.statistics]: 'Files',
  [filesExplorerPath]: 'Files',
  [routes.browse.trending]: 'Trending',
  [routes.public.trending]: 'Trending',
  [routes.trusted.trending]: 'Trending',
  [routes.browse.search]: 'Search',
  [routes.public.search]: 'Search',
  [routes.trusted.search]: 'Search',
  [routes.browse.searchTorrents]: 'Search torrents',
  [routes.public.searchTorrents]: 'Search torrents',
  [routes.trusted.searchTorrents]: 'Search torrents',
  [routes.sources.remoteCatalogs.statistics]: 'Remote catalogs',
  [routes.sources.remoteCatalogs.settings]: 'Remote catalogs',
  [routes.sources.remoteCatalogs.history]: 'Remote catalog history',
  [routes.connections.sharedCatalogs.settings]: 'Shared catalog settings',
  [routes.connections.stremio.history]: 'Stremio history',
  [routes.metadata.identification.statistics]: 'Identification',
  [identificationResultsPath]: 'Identification',
  [identificationCorrespondencesPath]: 'Correspondences',
  [routes.metadata.catalogs.videos.history]: 'Video history',
  [routes.metadata.catalogs.games.history]: 'Game history',
  [routes.metadata.catalogs.software.history]: 'Software history',
  [routes.metadata.catalogs.music.history]: 'Music history',
  [routes.metadata.catalogs.books.history]: 'Book history',
  [routes.metadata.identification.history]: 'Identification history',
  [routes.debrid.history]: 'Debrid history',
  [routes.administration.storage.statistics]: 'Storage',
  [routes.administration.logs]: 'Logs',
  // Legacy views retained for in-flight browser history state across upgrades.
  '/providers': 'Torrent statistics',
  '/providers/settings': 'All sources',
  '/runs': 'Torrent history',
  '/torrents': 'Files',
  '/files': 'Files',
  '/files/results': 'Files',
  '/sources/files/results': 'Files',
  '/trending': 'Trending',
  '/public/trending': 'Trending',
  '/trusted/trending': 'Trending',
  '/search': 'Search',
  '/public/search': 'Search',
  '/trusted/search': 'Search',
  '/search/torrents': 'Search torrents',
  '/public/search/torrents': 'Search torrents',
  '/trusted/search/torrents': 'Search torrents',
  '/remotes': 'Remote catalogs',
  '/remotes/settings': 'Remote catalogs',
  '/remotes/history': 'Remote catalog history',
  '/sharing/settings': 'Shared catalog settings',
  '/sharing/remotes': 'Remote catalogs',
  '/addon/history': 'Stremio history',
  '/metadata/history': 'Metadata history',
  '/metadata/identification': 'Identification',
  '/metadata/videos/history': 'Video history',
  '/metadata/games/history': 'Game history',
  '/metadata/software/history': 'Software history',
  '/metadata/music/history': 'Music history',
  '/metadata/books/history': 'Book history',
  '/health': 'Storage',
  '/logs': 'Logs',
}
type ListView = { pathname: string; search: string; returnView?: ListView }
export type BackLink = { to: string; label: string; state?: { returnView: ListView } }

function isLegacySourceSettingsPath(pathname: string) {
  return (
    !/^\/providers\/(?:new|settings)$/i.test(pathname) &&
    /^\/providers\/(?:id\/)?[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/i.test(pathname)
  )
}

function viewLabel(pathname: string) {
  if (Object.hasOwn(viewLabels, pathname)) return viewLabels[pathname]
  if (
    /^\/(?:sources\/(?:indexers\/torrents|remote-catalogs)\/history|runs|remotes\/history)\/[A-Za-z0-9_-]{1,128}$/.test(
      pathname,
    )
  )
    return 'Processed records'
  if (/^\/(?:sources\/indexers\/torrents\/history\/raw|raw)\/[1-9][0-9]{0,15}$/.test(pathname))
    return 'Processed record'
  if (isSourceSettingsPath(pathname) || isLegacySourceSettingsPath(pathname))
    return 'Source settings'
  if (/^\/(?:(?:public|trusted)\/)?(?:browse\/)?search\/[1-9][0-9]{0,15}$/.test(pathname))
    return 'Source releases'
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

export function detailBackLink(
  state: unknown,
  fallback?: BackLink,
  currentPathname?: string,
): BackLink | undefined {
  if (!fallback) return fallback
  let view = viewFromState(state)
  while (view && currentPathname && view.pathname === currentPathname) {
    view = view.returnView
  }
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
  const { pathname, state } = useLocation()
  return detailBackLink(state, fallback, pathname)
}

// Updating a detail tab or its filters must keep the originating list view.
// The state contains only the list's existing path and public search parameters.
export function useViewSearchParams() {
  const [search, setSearch] = useSearchParams()
  const { state } = useLocation()
  const update = useCallback<SetURLSearchParams>(
    (next, options) => {
      const focused = options?.flushSync ? document.activeElement : null
      let selection: readonly [number, number] | undefined
      try {
        if (
          (focused instanceof HTMLInputElement || focused instanceof HTMLTextAreaElement) &&
          focused.selectionStart !== null
        ) {
          selection = [focused.selectionStart, focused.selectionEnd ?? focused.selectionStart]
        }
      } catch {
        selection = undefined
      }
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
          ) {
            try {
              replacement.setSelectionRange(...selection)
            } catch {
              /* Non-text inputs do not support selection ranges. */
            }
          }
        }
      }
    },
    [setSearch, state],
  )
  return [search, update] as const
}
