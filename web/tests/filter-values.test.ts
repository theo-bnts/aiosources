import { test } from 'node:test'
import assert from 'node:assert/strict'
import { selectionMatches, selectionValues } from '../src/lib/filter-values.ts'

test('filter selections preserve old URLs, combine alternatives and keep unknown choices visible', () => {
  assert.deepEqual(selectionValues(''), [])
  assert.deepEqual(selectionValues('enabled'), ['enabled'])
  assert.deepEqual(selectionValues('enabled,disabled,enabled'), ['enabled', 'disabled'])
  assert.deepEqual(selectionValues('retired-source,current-source'), [
    'retired-source',
    'current-source',
  ])
  assert.equal(selectionMatches('', 'enabled'), true)
  assert.equal(selectionMatches('enabled,invalid', 'invalid'), true)
  assert.equal(selectionMatches('enabled,invalid', 'disabled'), false)
})
