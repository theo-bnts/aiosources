import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  createSettingsIndex,
  searchSettings,
  settingByID,
  settingFromHash,
  settingURL,
  settingsCatalog,
  settingsHash,
  visibleSettings,
} from '../src/lib/settings-catalog.ts'
import { runtimeSections } from '../src/lib/runtime-settings-definitions.ts'

test('each setting has a unique, resolvable destination and every operational field is covered', () => {
  assert.equal(new Set(settingsCatalog.map((setting) => setting.id)).size, settingsCatalog.length)
  for (const setting of settingsCatalog) {
    assert.equal(settingFromHash(settingsHash(setting.id)), setting)
    assert.ok(setting.title && setting.group)
    assert.ok(setting.route === '' || setting.route.startsWith('/'))
  }
  for (const [section, definition] of Object.entries(runtimeSections))
    for (const [field, label] of definition.fields)
      assert.equal(settingByID(`runtime.${section}.${field}`)?.title, label)
  assert.equal(settingFromHash('#setting=%E0%A4%A'), undefined)
  assert.equal(settingFromHash('#setting=unknown'), undefined)
  assert.equal(settingFromHash('#maintenance'), undefined)
})

test('settings matching ignores punctuation and accents, ranks names first and tolerates a small typo', () => {
  const index = createSettingsIndex(visibleSettings({ admin: true }))
  assert.equal(searchSettings(index, 'CACHE.validity')[0].id, 'debrid.cache')
  assert.equal(searchSettings(index, 'cache validty')[0].id, 'debrid.cache')
  assert.ok(
    searchSettings(index, 'réinitialiser').some((setting) => setting.id === 'storage.reset'),
  )
  assert.ok(
    searchSettings(index, 'queued torrents').some(
      (setting) => setting.id === 'runtime.identification.queue_batch',
    ),
  )
  assert.deepEqual(searchSettings(index, ' \t '), [])
  assert.deepEqual(searchSettings(index, '<unknown setting>'), [])
  assert.ok(searchSettings(index, 'cache').length > 1)
})

test('public search only includes enabled public controls and browser preferences', () => {
  assert.ok(visibleSettings({ admin: false }).every((setting) => setting.public === 'interface'))
  const publicSettings = visibleSettings({
    admin: false,
    stremio: true,
    sources: false,
    services: ['alldebrid'],
  })
  assert.ok(publicSettings.some((setting) => setting.id === 'visitor.stremio.ad'))
  assert.ok(
    publicSettings.every(
      (setting) => setting.public === 'interface' || setting.public === 'stremio',
    ),
  )
  assert.ok(
    publicSettings.every(
      (setting) =>
        !setting.entity && setting.title !== 'Sources' && setting.title !== 'TorBox API key',
    ),
  )
  assert.ok(
    visibleSettings({ admin: false, torznab: true, sources: true }).some(
      (setting) => setting.id === 'visitor.torznab.sources',
    ),
  )
  const adminSettings = visibleSettings({ admin: true })
  assert.ok(adminSettings.every((setting) => !setting.public || setting.public === 'interface'))
})

test('entity destinations encode identifiers and keep configuration selection separate from field targets', () => {
  const source = settingByID('source.timeout')!
  const url = new URL(settingURL(source, 'archive / one'), 'https://example.invalid')
  assert.equal(url.pathname, '/providers/archive%20%2F%20one')
  assert.equal(url.searchParams.get('configure'), 'archive / one')
  assert.equal(settingFromHash(url.hash), source)
  assert.equal(settingURL(source, 'new', true), '/providers/new')
  assert.equal(settingURL(source, 'new'), '/providers/id/new?configure=new#setting=source.timeout')
  assert.equal(
    settingURL(settingByID('imports.workers')!),
    '/providers/settings?settings=advanced#setting=imports.workers',
  )
  assert.equal(
    settingURL(settingByID('torznab.rate')!, 'new'),
    '/torznab?configure=new#setting=torznab.rate',
  )
})

test('catalog sharing and reception expose the same data menu and exact searchable leaf controls', () => {
  for (const entity of ['share', 'remote']) {
    for (const key of ['identifiers', 'languages', 'technical', 'files'])
      assert.deepEqual(settingByID(`${entity}.${key}`)?.reveal, ['Shared data', 'Torrents'])
    assert.deepEqual(settingByID(`${entity}.metadata`)?.reveal, ['Shared data', 'Metadata'])
    assert.deepEqual(settingByID(`${entity}.availability`)?.reveal, ['Shared data', 'Debrid'])
  }
  assert.deepEqual(settingByID('share.fields')?.reveal, ['Shared data', 'Torrents'])
})
