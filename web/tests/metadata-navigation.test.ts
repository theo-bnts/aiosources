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
  assert.equal(metadataOperationSection('metadata', 'openlibrary'), 'books')
  assert.equal(metadataOperationSection('matching', 'local'), 'identification')
  assert.equal(metadataHistoryPath('books'), '/metadata/books/history')
  assert.equal(
    legacyMetadataHistory('?service=openlibrary&kind=metadata&status=failed&offset=50'),
    '/metadata/books/history?status=failed&offset=50',
  )
  assert.equal(
    legacyMetadataHistory('?service=tmdb&status=succeeded'),
    '/metadata/videos/history?status=succeeded',
  )
  assert.equal(
    legacyMetadataHistory('?kind=matching&limit=25'),
    '/metadata/identification/history?limit=25',
  )
  assert.equal(legacyMetadataHistory(''), '/metadata/videos/history')
})

test('existing settings identifiers point to their separated metadata workspace', () => {
  for (const id of [
    'metadata.key',
    'metadata.rate',
    'metadata.sync',
    'metadata.interval',
    'runtime.metadata.workers',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/videos/settings#'))
  for (const id of ['metadata.books', 'metadata.book-interval'])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/books/settings#'))
  for (const id of [
    'metadata.match',
    'metadata.match-interval',
    'runtime.identification.queue_batch',
    'runtime.matching.ambiguity_margin',
  ])
    assert.ok(settingURL(settingByID(id)!).startsWith('/metadata/identification/settings#'))
})
