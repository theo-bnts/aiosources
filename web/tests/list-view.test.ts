import test from 'node:test'
import assert from 'node:assert/strict'
import { resetListView } from '../src/lib/list-view.ts'

test('clearing a list preserves page preferences and surrounding view without changing the original URL', () => {
  const original = new URLSearchParams('q=release&status=error&offset=100&limit=25&tab=records')
  const reset = resetListView(original, ['q', 'status'])
  assert.equal(reset.toString(), 'limit=25&tab=records')
  assert.equal(original.toString(), 'q=release&status=error&offset=100&limit=25&tab=records')
  const firstPage = resetListView(original)
  assert.equal(firstPage.toString(), 'q=release&status=error&limit=25&tab=records')
})

test('embedded source records reset independently of the parent run and its other lists', () => {
  const original = new URLSearchParams(
    'run=retained&tab=records&raw_q=release&raw_status=ignored&raw_offset=100&offset=50&limit=25',
  )
  assert.equal(
    resetListView(original, ['raw_q', 'raw_status'], 'raw_offset').toString(),
    'run=retained&tab=records&offset=50&limit=25',
  )
})
