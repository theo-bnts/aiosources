import { routes } from './routes.ts'

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
  | 'alerts'

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

  // Sources -> Indexers -> Torrents
  if (
    path === '/' ||
    path === routes.sources.base ||
    path === routes.sources.indexers.base ||
    path === routes.sources.indexers.torrents.base ||
    path === routes.sources.indexers.torrents.statistics ||
    path === '/providers' ||
    path === '/indexers'
  ) {
    return 'statistics'
  }
  if (
    path === routes.sources.indexers.torrents.settings ||
    path.startsWith(`${routes.sources.indexers.torrents.settings}/`) ||
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
    path.startsWith(`${routes.sources.indexers.torrents.raw}/`) ||
    path.startsWith(`${routes.sources.indexers.torrents.pages}/`) ||
    path.startsWith('/raw/') ||
    path.startsWith('/pages/')
  ) {
    return 'library'
  }
  if (
    path === routes.sources.indexers.torrents.history ||
    path.startsWith(`${routes.sources.indexers.torrents.history}/`) ||
    path === routes.sources.remoteCatalogs.history ||
    path.startsWith(`${routes.sources.remoteCatalogs.history}/`) ||
    path === '/runs' ||
    path.startsWith('/runs/') ||
    path === '/raw' ||
    path === '/remotes/history' ||
    path.startsWith('/remotes/history/')
  ) {
    return 'runs'
  }

  // Sources -> Remote catalogs
  if (
    path === routes.sources.remoteCatalogs.base ||
    path === routes.sources.remoteCatalogs.statistics ||
    path === routes.sources.remoteCatalogs.settings ||
    path === '/remotes' ||
    path === '/remotes/settings' ||
    path === '/sharing/remotes'
  ) {
    return 'remotes'
  }

  // Sources -> Files
  if (
    path === routes.sources.files.base ||
    path === routes.sources.files.statistics ||
    path === '/files'
  ) {
    return 'files-statistics'
  }
  if (path === routes.sources.files.results || path === '/files/results' || path === '/torrents') {
    return 'library'
  }

  // Browse -> Trending & Search (admin, public, trusted)
  if (
    path === routes.browse.base ||
    path === routes.browse.trending ||
    path === routes.public.trending ||
    path === routes.trusted.trending ||
    path === '/trending' ||
    path === '/public/trending' ||
    path === '/trusted/trending'
  ) {
    return 'catalog-trending'
  }
  if (
    path === routes.browse.searchTorrents ||
    path === routes.public.searchTorrents ||
    path === routes.trusted.searchTorrents ||
    path === '/search/torrents' ||
    path === '/public/search/torrents' ||
    path === '/trusted/search/torrents'
  ) {
    return 'catalog-torrents'
  }
  if (
    path === routes.browse.search ||
    path.startsWith(`${routes.browse.search}/`) ||
    path === routes.public.search ||
    path.startsWith(`${routes.public.search}/`) ||
    path === routes.trusted.search ||
    path.startsWith(`${routes.trusted.search}/`) ||
    path === '/search' ||
    path.startsWith('/search/') ||
    path === '/public/search' ||
    path.startsWith('/public/search/') ||
    path === '/trusted/search' ||
    path.startsWith('/trusted/search/')
  ) {
    return 'catalog-search'
  }

  // Metadata -> Identification
  if (
    path === routes.metadata.identification.base ||
    path === routes.metadata.identification.statistics
  ) {
    return 'identification-statistics'
  }
  if (path === routes.metadata.identification.results) return 'matching-diagnostics'
  if (path === routes.metadata.identification.correspondences) return 'release-correspondences'

  // Metadata / Debrid / Stremio settings
  if (
    path === routes.metadata.identification.settings ||
    path === routes.debrid.settings ||
    path === routes.connections.stremio.settings ||
    path === '/addon/settings' ||
    /^\/metadata\/(?:catalogs\/)?(videos|games|software|music|books)\/settings$/.test(path) ||
    path === '/metadata/settings'
  ) {
    return 'aiosource'
  }

  // Metadata / Debrid history
  if (
    path === routes.debrid.history ||
    path.startsWith(`${routes.debrid.history}/`) ||
    path === '/metadata/history' ||
    path.startsWith('/metadata/history/') ||
    /^\/metadata\/(?:catalogs\/)?(videos|games|software|music|books)\/history(\/|$)/.test(path) ||
    path === routes.metadata.identification.history ||
    path.startsWith(`${routes.metadata.identification.history}/`)
  ) {
    return 'operation-history'
  }

  // Metadata / Debrid statistics
  if (
    path === routes.debrid.base ||
    path === routes.debrid.statistics ||
    path === routes.metadata.base ||
    path === routes.metadata.catalogs.base ||
    /^\/metadata\/(?:catalogs\/)?(videos|games|software|music|books)(?:\/statistics)?$/.test(path)
  ) {
    return 'statistics'
  }

  // Connections
  if (
    path === routes.connections.stremio.base ||
    path === routes.connections.stremio.statistics ||
    path === routes.connections.stremio.history ||
    path.startsWith(`${routes.connections.stremio.history}/`) ||
    path === '/addon' ||
    path === '/addon/history' ||
    path.startsWith('/addon/history/')
  ) {
    return 'stremio-history'
  }
  if (
    path === routes.connections.sharedCatalogs.base ||
    path === routes.connections.sharedCatalogs.statistics ||
    path === routes.connections.sharedCatalogs.settings ||
    path === '/sharing' ||
    path === '/sharing/settings'
  ) {
    return 'sharing'
  }
  if (
    path === routes.connections.alerts.base ||
    path === routes.connections.alerts.active ||
    path === routes.connections.alerts.settings ||
    path === routes.trusted.alerts ||
    path === routes.public.alerts ||
    path === '/alerts'
  ) {
    return 'alerts'
  }
  if (
    path === routes.connections.base ||
    path === routes.connections.torznab ||
    path === '/torznab'
  ) {
    return 'torznab'
  }

  // Administration
  if (
    path === routes.administration.storage.base ||
    path === routes.administration.storage.statistics ||
    path === routes.administration.storage.settings ||
    path === '/health' ||
    path === '/health/settings'
  ) {
    return 'health'
  }
  if (
    path === routes.administration.logs ||
    path === '/logs' ||
    path === '/notifications' ||
    path === '/webhooks'
  ) {
    return 'logs'
  }
  if (
    path === routes.administration.network.outgoing.base ||
    path === routes.administration.network.outgoing.proxies ||
    path === routes.administration.network.outgoing.routing ||
    path === '/network/outgoing' ||
    path === '/network/outgoing/routing' ||
    path === '/network/proxies' ||
    path === '/network/routing'
  ) {
    return 'network'
  }
  if (
    path === routes.administration.network.base ||
    path === routes.administration.network.incoming ||
    path === '/network/incoming' ||
    path === '/network'
  ) {
    return 'incoming-network'
  }
  if (path === routes.administration.users.public || path === '/public-access') {
    return 'public-access'
  }
  if (
    path === routes.administration.users.base ||
    path === routes.administration.users.trusted ||
    path === '/trusted-users' ||
    path === '/users'
  ) {
    return 'trusted-users'
  }
  if (
    path === routes.administration.base ||
    path === routes.administration.security.base ||
    path.startsWith(`${routes.administration.security.base}/`) ||
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
