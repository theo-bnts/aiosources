import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  selectionGroupState,
  selectionLabels,
  toggleSelectionGroup,
} from '../src/lib/multi-select-values.ts'
import { catalogMediaTypes, stremioMediaTypes } from '../src/lib/connection-options.ts'

test('media families combine with individual types and preserve other choices', () => {
  const videos = catalogMediaTypes.filter((option) => option.group === 'Videos')
  const books = catalogMediaTypes.filter((option) => option.group === 'Books')
  assert.equal(selectionGroupState([], videos), false)
  assert.equal(selectionGroupState(['movie'], videos), 'indeterminate')
  const selected = toggleSelectionGroup(['ebook', 'game'], videos, true)
  assert.deepEqual(selected, ['ebook', 'game', 'movie', 'tv'])
  assert.equal(selectionGroupState(selected, videos), true)
  assert.equal(selectionGroupState(selected, books), 'indeterminate')
  assert.equal(selectionLabels(selected, catalogMediaTypes, true), 'Ebooks, Games, Videos')
  assert.deepEqual(toggleSelectionGroup(selected, videos, false), ['ebook', 'game'])
  assert.deepEqual(toggleSelectionGroup(['movie', 'movie'], videos, true), ['movie', 'tv'])
})

test('group labels do not alter stored types, flat controls or unknown selections', () => {
  const books = catalogMediaTypes.filter((option) => option.group === 'Books')
  const selected = toggleSelectionGroup(['retired-type'], books, true)
  assert.deepEqual(selected, ['retired-type', 'ebook', 'audiobook', 'book'])
  assert.equal(selectionLabels(selected, catalogMediaTypes, true), 'retired-type, Books')
  assert.equal(selectionLabels(['movie', 'tv'], catalogMediaTypes, false), 'Movies, Series')
  assert.equal(selectionGroupState([], []), false)
  const videos = toggleSelectionGroup([], stremioMediaTypes, true)
  assert.deepEqual(videos, ['movie', 'series', 'anime'])
  assert.equal(selectionLabels(videos, stremioMediaTypes, true), 'Videos')
  const allowed = catalogMediaTypes.filter((option) => option.value === 'ebook')
  assert.deepEqual(toggleSelectionGroup([], allowed, true), ['ebook'])
})
