import test from 'node:test'
import assert from 'node:assert/strict'
import { detailBackLink, listViewState } from '../src/lib/detail-navigation.ts'

test('detail return links retain the complete public list view and direct links keep their fallback', () => {
  const fallback = { to: '/runs', label: 'All runs' }
  assert.deepEqual(
    detailBackLink(
      listViewState({ pathname: '/runs', search: '?provider=example&limit=25&offset=50' }),
      fallback,
    ),
    { to: '/runs?provider=example&limit=25&offset=50', label: 'Sources history' },
  )
  assert.equal(detailBackLink(null, fallback), fallback)
  assert.equal(detailBackLink({ returnView: { pathname: '/', search: '' } }), undefined)
  assert.equal(listViewState({ pathname: '/secrets', search: '' }), undefined)
  assert.deepEqual(
    detailBackLink(
      listViewState({
        pathname: '/runs/run-one',
        search: '?tab=raw&raw_status=error&raw_offset=50',
      }),
      fallback,
    ),
    { to: '/runs/run-one?tab=raw&raw_status=error&raw_offset=50', label: 'Processed records' },
  )
})

test('return links reject external addresses, arbitrary routes and malformed browser state', () => {
  const fallback = { to: '/runs', label: 'All runs' }
  for (const state of [
    null,
    'invalid',
    { returnView: { pathname: '//outside.example', search: '' } },
    { returnView: { pathname: 'https://outside.example/runs', search: '' } },
    { returnView: { pathname: '/secrets', search: '' } },
    { returnView: { pathname: '/raw', search: '?run=one' } },
    { returnView: { pathname: '/runs/..', search: '' } },
    { returnView: { pathname: 'constructor', search: '' } },
    { returnView: { pathname: '/runs', search: 'https://outside.example' } },
    { returnView: { pathname: '/runs', search: '?q=example#unrelated' } },
    { returnView: { pathname: '/runs', search: 25 } },
  ])
    assert.equal(detailBackLink(state, fallback), fallback)
})
