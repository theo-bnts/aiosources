import test from 'node:test'
import assert from 'node:assert/strict'
import { sourceSettingsPath, isSourceSettingsPath } from '../src/lib/source-navigation.ts'

test('source identifiers cannot collide with navigation pages, including case variants', () => {
  for (const id of ['new', 'settings', 'NEW', 'Settings', 'nEw', 'sEtTiNgS']) {
    assert.equal(sourceSettingsPath(id), `/providers/id/${id}`)
    assert.equal(isSourceSettingsPath(sourceSettingsPath(id)), true)
    assert.equal(isSourceSettingsPath(`/providers/${id}`), false)
  }
})

test('ordinary source links and their identifiers remain unchanged', () => {
  for (const id of ['archive', 'id', 'Source_42', 'source-settings', 'new-source']) {
    assert.equal(sourceSettingsPath(id), `/providers/${id}`)
    assert.equal(isSourceSettingsPath(sourceSettingsPath(id)), true)
  }
  assert.equal(isSourceSettingsPath('/providers/id/new/extra'), false)
  assert.equal(isSourceSettingsPath('/providers/..'), false)
  assert.equal(isSourceSettingsPath('//outside.example/providers/source'), false)
})
