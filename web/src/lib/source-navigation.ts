import { routes } from './routes.ts'

export const filesPath = routes.sources.files.statistics
export const filesExplorerPath = routes.sources.files.explorer

const sourceSettingsPrefix = routes.sources.indexers.torrents.settings

// Source identifiers remain data when they match a directory or action name.
// Ordinary URLs stay stable; only conflicting identifiers need another path.
export function sourceSettingsPath(id: string) {
  if (id === '.' || id === '..') return routes.sources.indexers.torrents.statistics
  const prefix = ['new', 'settings'].includes(id.toLowerCase())
    ? `${sourceSettingsPrefix}/id/`
    : `${sourceSettingsPrefix}/`
  return prefix + encodeURIComponent(id)
}

export function isSourceSettingsPath(pathname: string) {
  return (
    !/^\/sources\/indexers\/torrents\/settings\/(?:new|settings)$/i.test(pathname) &&
    /^\/sources\/indexers\/torrents\/settings\/(?:id\/)?[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/i.test(
      pathname,
    )
  )
}
