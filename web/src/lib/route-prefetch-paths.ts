export type RouteChunkKey =
  | 'statistics'
  | 'providers'
  | 'runs'
  | 'files-statistics'
  | 'library'
  | 'matching-diagnostics'
  | 'release-correspondences'
  | 'identification-statistics'
  | 'stremio-history'
  | 'logs'
  | 'network'
  | 'incoming-network'
  | 'sharing'
  | 'remotes'
  | 'public-access'
  | 'trusted-users'
  | 'torznab'
  | 'health'
  | 'security'
  | 'aiosource'
  | 'catalog-search'
  | 'catalog-trending'
  | 'catalog-torrents'
  | 'operation-history'

export const PRIMARY_ADMIN_ROUTE_CHUNKS: readonly RouteChunkKey[] = [
  'statistics',
  'files-statistics',
  'catalog-trending',
  'catalog-search',
  'identification-statistics',
  'matching-diagnostics',
  'release-correspondences',
  'stremio-history',
  'health',
  'logs',
  'operation-history',
  'aiosource',
]

function normalizePathname(rawPath: string): string {
  const withoutHash = rawPath.split('#', 1)[0] ?? ''
  const withoutQuery = withoutHash.split('?', 1)[0] ?? ''
  if (!withoutQuery || withoutQuery === '/') return '/'
  return withoutQuery.length > 1 && withoutQuery.endsWith('/')
    ? withoutQuery.slice(0, -1)
    : withoutQuery
}

export function resolveRouteChunkKey(rawPath: string): RouteChunkKey | null {
  const path = normalizePathname(rawPath)

  if (path === '/' || path === '/providers' || path === '/indexers') return 'statistics'
  if (
    path === '/providers/settings' ||
    path === '/providers/new' ||
    path.startsWith('/providers/') ||
    path === '/settings' ||
    path === '/schedules' ||
    path === '/import-settings'
  ) {
    return 'providers'
  }
  if (
    path === '/runs' ||
    path.startsWith('/runs/') ||
    path === '/raw' ||
    path === '/remotes/history' ||
    path.startsWith('/remotes/history/')
  ) {
    return 'runs'
  }
  if (path === '/files') return 'files-statistics'
  if (
    path === '/files/results' ||
    path === '/torrents' ||
    path.startsWith('/raw/') ||
    path.startsWith('/pages/')
  ) {
    return 'library'
  }

  if (path === '/trending' || path === '/public/trending' || path === '/trusted/trending') {
    return 'catalog-trending'
  }
  if (
    path === '/search/torrents' ||
    path === '/public/search/torrents' ||
    path === '/trusted/search/torrents'
  ) {
    return 'catalog-torrents'
  }
  if (
    path === '/search' ||
    path.startsWith('/search/') ||
    path === '/public/search' ||
    path.startsWith('/public/search/') ||
    path === '/trusted/search' ||
    path.startsWith('/trusted/search/')
  ) {
    return 'catalog-search'
  }

  if (path === '/metadata/identification') return 'identification-statistics'
  if (path === '/metadata/identification/results') return 'matching-diagnostics'
  if (path === '/metadata/identification/correspondences') return 'release-correspondences'

  if (
    path === '/metadata/identification/settings' ||
    path === '/debrid/settings' ||
    path === '/addon/settings' ||
    /^\/metadata\/(videos|games|software|music|books)\/settings$/.test(path) ||
    path === '/metadata/settings'
  ) {
    return 'aiosource'
  }
  if (
    path === '/debrid/history' ||
    path.startsWith('/debrid/history/') ||
    path === '/metadata/history' ||
    path.startsWith('/metadata/history/') ||
    /^\/metadata\/(videos|games|software|music|books|identification)\/history(\/|$)/.test(path)
  ) {
    return 'operation-history'
  }
  if (
    path === '/debrid' ||
    path === '/metadata' ||
    path === '/metadata/catalogs' ||
    /^\/metadata\/(videos|games|software|music|books)$/.test(path)
  ) {
    return 'statistics'
  }

  if (path === '/addon' || path === '/addon/history' || path.startsWith('/addon/history/')) {
    return 'stremio-history'
  }
  if (path === '/health' || path === '/health/settings') return 'health'
  if (path === '/logs' || path === '/notifications' || path === '/webhooks') return 'logs'
  if (
    path === '/network/outgoing' ||
    path === '/network/outgoing/routing' ||
    path === '/network/proxies' ||
    path === '/network/routing'
  ) {
    return 'network'
  }
  if (path === '/network/incoming' || path === '/network') return 'incoming-network'
  if (path === '/sharing' || path === '/sharing/settings') return 'sharing'
  if (path === '/remotes' || path === '/remotes/settings' || path === '/sharing/remotes') {
    return 'remotes'
  }
  if (path === '/public-access') return 'public-access'
  if (path === '/trusted-users' || path === '/users') return 'trusted-users'
  if (path === '/torznab') return 'torznab'
  if (
    path === '/security' ||
    path.startsWith('/security/') ||
    path === '/secrets' ||
    path === '/public-access/limits' ||
    path === '/network/limits'
  ) {
    return 'security'
  }

  return null
}
