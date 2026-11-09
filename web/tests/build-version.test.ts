import test from 'node:test'
import assert from 'node:assert/strict'
import { buildVersion } from '../src/lib/build-version.ts'

test('published versions link to the same build shown in GitHub releases', () => {
  assert.deepEqual(buildVersion('2.3.0.977dd1727684'), {
    display: '2.3.0.977dd1727684',
    release: '2.3.0',
    build: '977dd1727684',
    url: 'https://github.com/theo-bnts/aiosources/releases/tag/build-977dd1727684',
  })
})

test('local versions and unrecognized metadata cannot create arbitrary links', () => {
  for (const value of [
    undefined,
    '__AIOSOURCES_VERSION__',
    'dev',
    '2.3.0',
    '2.3.0-custom',
    'https://outside.example',
    '2.3.0.<script>unexpected()</script>',
    '2.3.0.977dd1727684/extra',
  ]) {
    assert.equal(buildVersion(value).url, undefined)
  }
  assert.equal(buildVersion(undefined).display, 'dev')
  assert.equal(buildVersion('x'.repeat(500)).display.length, 200)
})
