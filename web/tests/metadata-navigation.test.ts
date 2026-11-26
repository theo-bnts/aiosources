import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  legacyMetadataHistory,
  metadataHistoryPath,
  metadataOperationSection,
} from '../src/lib/metadata-navigation.ts'
import { settingByID, settingURL } from '../src/lib/settings-catalog.ts'

test('metadata histories separate video, book and shared identification operations', () => {
  assert.equal(metadataOperationSection('metadata', 'tmdb'), 'videos')
  assert.equal(metadataOperationSection('metadata', 'igdb'), 'games')
  assert.equal(metadataHistoryPath('games'), '/metadata/catalogs/games/history')
  assert.equal(metadataOperationSection('metadata', 'software'), 'software')
  assert.equal(metadataHistoryPath('software'), '/metadata/catalogs/software/history')
  assert.equal(metadataOperationSection('metadata', 'openlibrary'), 'books')
  assert.equal(metadataOperationSection('metadata', 'musicbrainz'), 'music')
  assert.equal(metadataHistoryPath('music'), '/metadata/catalogs/music/history')
  assert.equal(metadataOperationSection('matching', 'local'), 'identification')
  assert.equal(metadataHistoryPath('books'), '/metadata/catalogs/books/history')
  assert.equal(
    legacyMetadataHistory('?service=software&kind=metadata&status=succeeded'),
    '/metadata/catalogs/software/history?status=succeeded',
  )
  assert.equal(
    legacyMetadataHistory('?service=musicbrainz&kind=metadata&status=succeeded'),
    '/metadata/catalogs/music/history?status=succeeded',
  )
  assert.equal(
    legacyMetadataHistory('?service=openlibrary&kind=metadata&status=failed&offset=50'),
    '/metadata/catalogs/books/history?status=failed&offset=50',
  )
  assert.equal(
    legacyMetadataHistory('?service=tmdb&status=succeeded'),
    '/metadata/catalogs/videos/history?status=succeeded',
  )
  assert.equal(
    legacyMetadataHistory('?kind=matching&limit=25'),
    '/metadata/identification/history?limit=25',
  )
  assert.equal(legacyMetadataHistory(''), '/metadata/catalogs/videos/history')
})

test('existing settings identifiers point to their separated metadata workspace', () => {
  for (const id of [
    'metadata.key',
    'metadata.rate',
    'metadata.sync',
    'metadata.interval',
    'runtime.metadata.workers',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/catalogs/videos/settings#'))
  for (const id of [
    'metadata.games-client',
    'metadata.games-key',
    'metadata.games-auto',
    'metadata.games-interval',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/catalogs/games/settings#'))
  for (const id of [
    'metadata.software-key',
    'metadata.software',
    'metadata.software-interval',
    'metadata.software-nvd',
    'metadata.software-plugins',
    'metadata.software-os',
    'metadata.software-icons',
    'metadata.software-platforms',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/catalogs/software/settings#'))
  for (const id of ['metadata.books', 'metadata.book-interval'])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/catalogs/books/settings#'))
  for (const id of [
    'metadata.music-key',
    'metadata.music',
    'metadata.music-interval',
    'metadata.music-official',
    'metadata.music-spoken',
    'metadata.music-covers',
    'metadata.music-types',
    'metadata.music-languages',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/catalogs/music/settings#'))
  for (const id of [
    'metadata.match',
    'metadata.match-interval',
    'runtime.identification.queue_batch',
    'runtime.matching.ambiguity_margin',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/identification/settings#'))
})
