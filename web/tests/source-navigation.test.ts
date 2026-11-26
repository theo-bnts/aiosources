import test from 'node:test'
import assert from 'node:assert/strict'
import { sourceSettingsPath, isSourceSettingsPath } from '../src/lib/source-navigation.ts'

test('source identifiers cannot collide with navigation pages, including case variants', () => {
  for (const id of ['new', 'settings', 'NEW', 'Settings', 'nEw', 'sEtTiNgS']) {
    assert.equal(sourceSettingsPath(id), `/sources/indexers/torrents/settings/id/${id}`)
    assert.equal(isSourceSettingsPath(sourceSettingsPath(id)), true)
    assert.equal(isSourceSettingsPath(`/sources/indexers/torrents/settings/${id}`), false)
  }
})

test('ordinary source links and their identifiers remain unchanged', () => {
  for (const id of ['archive', 'id', 'Source_42', 'source-settings', 'new-source']) {
    assert.equal(sourceSettingsPath(id), `/sources/indexers/torrents/settings/${id}`)
    assert.equal(isSourceSettingsPath(sourceSettingsPath(id)), true)
  }
  assert.equal(isSourceSettingsPath('/sources/indexers/torrents/settings/id/new/extra'), false)
  assert.equal(isSourceSettingsPath('/sources/indexers/torrents/settings/..'), false)
  assert.equal(
    isSourceSettingsPath('//outside.example/sources/indexers/torrents/settings/source'),
    false,
  )
})
