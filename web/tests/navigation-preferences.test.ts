import test from 'node:test'
import assert from 'node:assert/strict'
import { readNavigationPreferences } from '../src/lib/navigation-preferences.ts'

function storage(entries: [string, string][]) {
  const values = new Map(entries)
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
    removeItem: (key: string) => void values.delete(key),
  }
}

test('navigation naming upgrades preserve boolean choices without copying unrelated data', () => {
  const saved = storage([
    ['ingest.navigation.groups', '{"sources":false,"metadata":true,"unrelated":"private"}'],
  ])
  assert.deepEqual(readNavigationPreferences(saved, 'aiosources.navigation.groups'), {
    sources: false,
    metadata: true,
  })
  assert.equal(saved.getItem('ingest.navigation.groups'), null)
  assert.equal(saved.getItem('aiosources.navigation.groups'), '{"sources":false,"metadata":true}')
})

test('current choices win and public navigation never inherits administrator choices', () => {
  const saved = storage([
    ['ingest.navigation.groups', '{"sources":false}'],
    ['aiosources.navigation.groups', '{"sources":true}'],
  ])
  assert.deepEqual(readNavigationPreferences(saved, 'aiosources.navigation.groups'), {
    sources: true,
  })
  assert.equal(readNavigationPreferences(saved, 'aiosources.public.navigation.groups'), undefined)
})

test('unavailable storage retains the previous choices for this page', () => {
  const saved = storage([['ingest.navigation.groups', '{"sources":false}']])
  assert.deepEqual(
    readNavigationPreferences(
      {
        ...saved,
        setItem: () => {
          throw new Error('full')
        },
      },
      'aiosources.navigation.groups',
    ),
    { sources: false },
  )
  assert.notEqual(saved.getItem('ingest.navigation.groups'), null)
})
