import assert from 'node:assert/strict'
import test from 'node:test'

import {
  PRIMARY_ADMIN_ROUTE_CHUNKS,
  resolveRouteChunkKey,
} from '../src/lib/route-prefetch-paths.ts'

test('resolveRouteChunkKey maps core admin routes, tabs, and detail paths', () => {
  const cases: [string, string | null][] = [
    ['/', 'statistics'],
    ['/providers', 'statistics'],
    ['/providers/settings', 'providers'],
    ['/providers/new', 'providers'],
    ['/providers/id/src-1', 'providers'],
    ['/runs', 'runs'],
    ['/runs/run-123?tab=raw', 'runs'],
    ['/files', 'files-statistics'],
    ['/files/results?q=show', 'library'],
    ['/raw/42', 'library'],
    ['/pages/99', 'library'],
    ['/trending', 'catalog-trending'],
    ['/trending?period=year', 'catalog-trending'],
    ['/public/trending', 'catalog-trending'],
    ['/trusted/trending', 'catalog-trending'],
    ['/search', 'catalog-search'],
    ['/search/42', 'catalog-search'],
    ['/public/search/42', 'catalog-search'],
    ['/search/torrents?q=matrix', 'catalog-torrents'],
    ['/trusted/search/torrents', 'catalog-torrents'],
    ['/metadata/videos', 'statistics'],
    ['/metadata/books/settings', 'aiosource'],
    ['/metadata/games/history', 'operation-history'],
    ['/metadata/identification', 'identification-statistics'],
    ['/metadata/identification/results?match_status=unmatched', 'matching-diagnostics'],
    ['/metadata/identification/correspondences', 'release-correspondences'],
    ['/metadata/identification/settings', 'aiosource'],
    ['/metadata/identification/history/op-1', 'operation-history'],
    ['/debrid', 'statistics'],
    ['/debrid/settings', 'aiosource'],
    ['/debrid/history', 'operation-history'],
    ['/addon', 'stremio-history'],
    ['/addon/settings', 'aiosource'],
    ['/addon/history/12', 'stremio-history'],
    ['/health', 'health'],
    ['/health/settings', 'health'],
    ['/logs', 'logs'],
    ['/network/incoming', 'incoming-network'],
    ['/network/outgoing/routing', 'network'],
    ['/sharing', 'sharing'],
    ['/remotes/settings', 'remotes'],
    ['/remotes/history', 'runs'],
    ['/torznab', 'torznab'],
    ['/trusted-users', 'trusted-users'],
    ['/public-access', 'public-access'],
    ['/security/limits#public-request-limits', 'security'],
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
