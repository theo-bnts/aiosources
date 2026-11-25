import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildMetadataFilters,
  formatMetadataFiltersSummary,
  parseMetadataCategories,
  suggestedProviderCategories,
} from '../src/lib/metadata-filters.ts'
import type { Provider } from '../src/lib/types.ts'

test('parseMetadataCategories splits, trims, deduplicates ignoring case, and validates bounds', () => {
  assert.deepEqual(parseMetadataCategories(''), { values: [], error: '' })
  assert.deepEqual(parseMetadataCategories(' 2000 , 5000\nFilms, films , 2000 '), {
    values: ['2000', '5000', 'Films'],
    error: '',
  })
  assert.match(parseMetadataCategories('bad\x00cat').error, /control characters/i)
  assert.match(parseMetadataCategories('x'.repeat(257)).error, /256 bytes/i)
  const tooMany = Array.from({ length: 101 }, (_, i) => `cat-${i}`).join(',')
  assert.match(parseMetadataCategories(tooMany).error, /100 categories/i)
})

test('buildMetadataFilters omits empty payloads and formats readable summaries', () => {
  assert.equal(buildMetadataFilters([], []), undefined)
  const filters = buildMetadataFilters(['2000', '5000'], ['unmatched', 'ambiguous'])
  assert.deepEqual(filters, {
    categories: ['2000', '5000'],
    identification_statuses: ['unmatched', 'ambiguous'],
  })
  assert.deepEqual(formatMetadataFiltersSummary(filters), [
    'Identification: Not identified, Needs review',
    'Categories: 2000, 5000',
  ])
})

test('suggestedProviderCategories collects unique categories from scopes and search config', () => {
  const provider = {
    search: { categories: [2000, 5000] },
    traversal: {
      window_pages: 10,
      total_paths: ['/total'],
      scopes: [
        { id: 'movies', match: { category: 'Films' } },
        { id: 'tv', match: { categories: ['Séries', '2000'] } },
      ],
    },
  } as unknown as Provider
  assert.deepEqual(suggestedProviderCategories(provider), ['Films', 'Séries', '2000', '5000'])
})
