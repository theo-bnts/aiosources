import test from 'node:test'
import assert from 'node:assert/strict'
import {
  changesSettingsEditor,
  settingsEditorDestination,
  settingsEditorNavigationKey,
} from '../src/lib/settings-editor.ts'

const current = {
  pathname: '/network/outgoing',
  search: '?configure=proxy-alpha',
  hash: '#setting=proxy.name',
}
const editor = { entity: 'proxy' as const, id: 'proxy-alpha' }

test('directory filters do not replace the selected settings editor', () => {
  const next = { ...current, search: `${current.search}&q=gateway&state=disabled` }
  assert.equal(settingsEditorNavigationKey(current), settingsEditorNavigationKey(next))
  assert.equal(changesSettingsEditor(current, next, editor), false)
})

test('a different field in the same editor preserves its draft and identity', () => {
  const next = { ...current, hash: '#setting=proxy.username' }
  assert.deepEqual(settingsEditorDestination(next), editor)
  assert.equal(changesSettingsEditor(current, next, editor), false)
})

test('history changes between editors and returning to the directory protect drafts', () => {
  assert.equal(
    changesSettingsEditor(current, { ...current, search: '?configure=proxy-beta' }, editor),
    true,
  )
  assert.equal(changesSettingsEditor(current, { ...current, search: '', hash: '' }, editor), true)
})

test('an explicit settings-search visit can reopen the same destination after closing it', () => {
  const previous = { ...current, state: { settingsNavigationId: 'previous' } }
  const next = { ...current, state: { settingsNavigationId: 'next' } }
  assert.notEqual(settingsEditorNavigationKey(previous), settingsEditorNavigationKey(next))
  assert.equal(changesSettingsEditor(previous, next, editor), false)
})

test('searching a field in an already open new editor does not discard it', () => {
  const empty = { ...current, search: '', hash: '' }
  const next = { ...current, search: '?configure=new', hash: '#setting=proxy.username' }
  assert.equal(changesSettingsEditor(empty, next, { entity: 'proxy', id: 'new' }), false)
})

test('browser preferences preserve the underlying connection editor', () => {
  const next = { ...current, hash: '#setting=interface.page-size' }
  // Use the catalog's public interface control rather than an unknown hash.
  assert.equal(changesSettingsEditor(current, next, editor), false)
})

test('settings links cannot open an editor for another route or an unknown setting', () => {
  assert.equal(settingsEditorDestination({ ...current, pathname: '/torznab' }), undefined)
  assert.equal(settingsEditorDestination({ ...current, hash: '#setting=unknown.field' }), undefined)
  assert.equal(settingsEditorDestination({ ...current, search: '' }), undefined)
})
