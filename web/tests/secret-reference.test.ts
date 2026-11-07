import { test } from 'node:test'
import assert from 'node:assert/strict'
import { newSecretReference, sourceCredentialPurpose } from '../src/lib/secret-reference.ts'

test('new credential names retain the service and purpose with a collision-resistant suffix', () => {
  for (const [prefix, expected] of [
    ['AllDebrid API key', 'alldebrid_api_key'],
    ['Archive-main_announce_url', 'archive_main_announce_url'],
    ['École / source_passkey', 'ecole_source_passkey'],
    ['!!!', 'secret'],
  ]) {
    const first = newSecretReference(prefix)
    const second = newSecretReference(prefix)
    assert.match(first, new RegExp(`^${expected}_[0-9a-f]{32}$`))
    assert.notEqual(first, second)
  }
  assert.equal(newSecretReference('a'.repeat(200)).length, 128)
})

test('source credential purposes follow the authentication field and parameter', () => {
  assert.equal(sourceCredentialPurpose('secret_ref', 'query', 'passkey'), 'passkey')
  assert.equal(sourceCredentialPurpose('secret_ref', 'header', 'X-Api-Key'), 'api_key')
  assert.equal(sourceCredentialPurpose('secret_ref', 'bearer', ''), 'api_key')
  assert.equal(sourceCredentialPurpose('secret_ref', 'header', 'X-Token'), 'token')
  assert.equal(sourceCredentialPurpose('secret_ref', 'cookie', 'session'), 'cookie')
  assert.equal(sourceCredentialPurpose('username_ref', 'basic', ''), 'username')
  assert.equal(sourceCredentialPurpose('password_ref', 'basic', ''), 'password')
  assert.equal(sourceCredentialPurpose('secret_ref', 'none', ''), 'credential')
})
