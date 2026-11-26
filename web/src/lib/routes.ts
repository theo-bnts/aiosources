export type VisitorPrefix = '/public' | '/trusted'

export const routes = {
  login: '/login',
  sources: {
    base: '/sources',
    indexers: {
      base: '/sources/indexers',
      torrents: {
        base: '/sources/indexers/torrents',
        statistics: '/sources/indexers/torrents/statistics',
        settings: '/sources/indexers/torrents/settings',
        newSource: '/sources/indexers/torrents/settings/new',
        history: '/sources/indexers/torrents/history',
        raw: '/sources/indexers/torrents/history/raw',
        pages: '/sources/indexers/torrents/history/pages',
      },
    },
    remoteCatalogs: {
      base: '/sources/remote-catalogs',
      statistics: '/sources/remote-catalogs/statistics',
      settings: '/sources/remote-catalogs/settings',
      history: '/sources/remote-catalogs/history',
    },
    files: {
      base: '/sources/files',
      statistics: '/sources/files/statistics',
      results: '/sources/files/results',
    },
  },
  metadata: {
    base: '/metadata',
    catalogs: {
      base: '/metadata/catalogs',
      videos: {
        base: '/metadata/catalogs/videos',
        statistics: '/metadata/catalogs/videos/statistics',
        settings: '/metadata/catalogs/videos/settings',
        history: '/metadata/catalogs/videos/history',
      },
      games: {
        base: '/metadata/catalogs/games',
        statistics: '/metadata/catalogs/games/statistics',
        settings: '/metadata/catalogs/games/settings',
        history: '/metadata/catalogs/games/history',
      },
      software: {
        base: '/metadata/catalogs/software',
        statistics: '/metadata/catalogs/software/statistics',
        settings: '/metadata/catalogs/software/settings',
        history: '/metadata/catalogs/software/history',
      },
      music: {
        base: '/metadata/catalogs/music',
        statistics: '/metadata/catalogs/music/statistics',
        settings: '/metadata/catalogs/music/settings',
        history: '/metadata/catalogs/music/history',
      },
      books: {
        base: '/metadata/catalogs/books',
        statistics: '/metadata/catalogs/books/statistics',
        settings: '/metadata/catalogs/books/settings',
        history: '/metadata/catalogs/books/history',
      },
    },
    identification: {
      base: '/metadata/identification',
      statistics: '/metadata/identification/statistics',
      results: '/metadata/identification/results',
      correspondences: '/metadata/identification/correspondences',
      settings: '/metadata/identification/settings',
      history: '/metadata/identification/history',
    },
  },
  debrid: {
    base: '/debrid',
    statistics: '/debrid/statistics',
    settings: '/debrid/settings',
    history: '/debrid/history',
  },
  browse: {
    base: '/browse',
    trending: '/browse/trending',
    search: '/browse/search',
    searchTorrents: '/browse/search/torrents',
  },
  connections: {
    base: '/connections',
    torznab: '/connections/torznab',
    stremio: {
      base: '/connections/stremio',
      statistics: '/connections/stremio/statistics',
      settings: '/connections/stremio/settings',
      history: '/connections/stremio/history',
    },
    sharedCatalogs: {
      base: '/connections/shared-catalogs',
      statistics: '/connections/shared-catalogs/statistics',
      settings: '/connections/shared-catalogs/settings',
    },
  },
  administration: {
    base: '/administration',
    security: {
      base: '/administration/security',
      secrets: '/administration/security/secrets',
      limits: '/administration/security/limits',
      activeSessions: '/administration/security/active-sessions',
    },
    network: {
      base: '/administration/network',
      incoming: '/administration/network/incoming',
      outgoing: {
        base: '/administration/network/outgoing',
        proxies: '/administration/network/outgoing/proxies',
        routing: '/administration/network/outgoing/routing',
      },
    },
    users: {
      base: '/administration/users',
      trusted: '/administration/users/trusted',
      public: '/administration/users/public',
    },
    storage: {
      base: '/administration/storage',
      statistics: '/administration/storage/statistics',
      settings: '/administration/storage/settings',
    },
    logs: '/administration/logs',
  },
  public: {
    base: '/public',
    trending: '/public/browse/trending',
    search: '/public/browse/search',
    searchTorrents: '/public/browse/search/torrents',
    torznab: '/public/connections/torznab',
    stremio: '/public/connections/stremio',
    sharedCatalogs: '/public/connections/shared-catalogs',
  },
  trusted: {
    base: '/trusted',
    trending: '/trusted/browse/trending',
    search: '/trusted/browse/search',
    searchTorrents: '/trusted/browse/search/torrents',
    torznab: '/trusted/connections/torznab',
    stremio: '/trusted/connections/stremio',
    sharedCatalogs: '/trusted/connections/shared-catalogs',
  },
} as const

export function visitorRoutes(prefix: VisitorPrefix) {
  return prefix === '/trusted' ? routes.trusted : routes.public
}

export function runDetailPath(id: string, remote = false): string {
  const base = remote
    ? routes.sources.remoteCatalogs.history
    : routes.sources.indexers.torrents.history
  return `${base}/${encodeURIComponent(id)}`
}

export function rawRecordPath(id: string | number): string {
  return `${routes.sources.indexers.torrents.raw}/${encodeURIComponent(String(id))}`
}

export function rawPagePath(id: string | number): string {
  return `${routes.sources.indexers.torrents.pages}/${encodeURIComponent(String(id))}`
}

export function stremioRequestPath(id: string | number): string {
  return `${routes.connections.stremio.history}/${encodeURIComponent(String(id))}`
}
