import assert from 'node:assert/strict'
import test from 'node:test'

import {
  PRIMARY_ADMIN_ROUTE_CHUNKS,
  resolveRouteChunkKey,
} from '../src/lib/route-prefetch-paths.ts'

test('resolveRouteChunkKey maps canonical and legacy admin/visitor routes, tabs, and detail paths', () => {
  const cases: [string, string | null][] = [
    ['/', 'statistics'],
    ['/sources/indexers/torrents/statistics', 'statistics'],
    ['/sources/indexers/torrents/settings', 'providers'],
    ['/sources/indexers/torrents/settings/new', 'providers'],
    ['/sources/indexers/torrents/settings/id/src-1', 'providers'],
    ['/sources/indexers/torrents/history', 'runs'],
    ['/sources/indexers/torrents/history/run-123?tab=raw', 'runs'],
    ['/sources/indexers/torrents/history/raw/42', 'library'],
    ['/sources/indexers/torrents/history/pages/99', 'library'],
    ['/sources/remote-catalogs/statistics', 'remotes'],
    ['/sources/remote-catalogs/settings', 'remotes'],
    ['/sources/remote-catalogs/history', 'runs'],
    ['/sources/files/statistics', 'files-statistics'],
    ['/sources/files/results?q=show', 'library'],
    ['/metadata/catalogs/videos/statistics', 'statistics'],
    ['/metadata/catalogs/books/settings', 'aiosource'],
    ['/metadata/catalogs/games/history', 'operation-history'],
    ['/metadata/identification/statistics', 'identification-statistics'],
    ['/metadata/identification/results?match_status=unmatched', 'matching-diagnostics'],
    ['/metadata/identification/correspondences', 'release-correspondences'],
    ['/metadata/identification/settings', 'aiosource'],
    ['/metadata/identification/history/op-1', 'operation-history'],
    ['/debrid/statistics', 'statistics'],
    ['/debrid/settings', 'aiosource'],
    ['/debrid/history', 'operation-history'],
    ['/browse/trending', 'catalog-trending'],
    ['/browse/trending?period=year', 'catalog-trending'],
    ['/public/browse/trending', 'catalog-trending'],
    ['/trusted/browse/trending', 'catalog-trending'],
    ['/browse/search', 'catalog-search'],
    ['/browse/search/42', 'catalog-search'],
    ['/public/browse/search/42', 'catalog-search'],
    ['/browse/search/torrents?q=matrix', 'catalog-torrents'],
    ['/trusted/browse/search/torrents', 'catalog-torrents'],
    ['/connections/torznab', 'torznab'],
    ['/connections/stremio/statistics', 'stremio-history'],
    ['/connections/stremio/settings', 'aiosource'],
    ['/connections/stremio/history/12', 'stremio-history'],
    ['/connections/shared-catalogs/statistics', 'sharing'],
    ['/connections/shared-catalogs/settings', 'sharing'],
    ['/connections/alerts', 'alerts'],
    ['/connections/alerts/active', 'alerts'],
    ['/connections/alerts/settings', 'alerts'],
    ['/trusted/connections/alerts', 'alerts'],
    ['/administration/security/secrets', 'security'],
    ['/administration/security/limits#public-request-limits', 'security'],
    ['/administration/security/active-sessions', 'security'],
    ['/administration/network/incoming', 'incoming-network'],
    ['/administration/network/outgoing/proxies', 'network'],
    ['/administration/network/outgoing/routing', 'network'],
    ['/administration/users/trusted', 'trusted-users'],
    ['/administration/users/public', 'public-access'],
    ['/administration/storage/statistics', 'health'],
    ['/administration/storage/settings', 'health'],
    ['/administration/logs', 'logs'],
    // Legacy paths remain mapped for smooth redirect prefetching
    ['/providers', 'statistics'],
    ['/providers/settings', 'providers'],
    ['/runs/run-123', 'runs'],
    ['/raw/42', 'library'],
    ['/pages/99', 'library'],
    ['/files', 'files-statistics'],
    ['/files/results', 'library'],
    ['/metadata/videos', 'statistics'],
    ['/addon', 'stremio-history'],
    ['/health', 'health'],
    ['/logs', 'logs'],
    ['/trusted-users', 'trusted-users'],
    ['/public-access', 'public-access'],
    ['/login', null],
    ['/unknown-route', null],
  ]

  for (const [path, expected] of cases) {
    assert.equal(resolveRouteChunkKey(path), expected, `unexpected chunk for ${path}`)
  }
})

test('PRIMARY_ADMIN_ROUTE_CHUNKS excludes heavy editor modules and avoids duplicates', () => {
  const unique = new Set(PRIMARY_ADMIN_ROUTE_CHUNKS)
  assert.equal(unique.size, PRIMARY_ADMIN_ROUTE_CHUNKS.length)
  assert.equal(unique.has('providers'), false)
  assert.equal(unique.has('library'), false)
  assert.equal(unique.has('catalog-trending'), true)
  assert.equal(unique.has('health'), true)
})
