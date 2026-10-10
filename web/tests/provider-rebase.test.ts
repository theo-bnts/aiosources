import test from 'node:test'
import assert from 'node:assert/strict'
import { rebaseProviderJSON } from '../src/lib/provider-json.ts'

test('source edits merge with concurrent settings without rounding numbers or rewriting custom cron', () => {
  const base =
    '{"name":"Before","schedule":{"cron":" 15  5 * * 1-5 ","max_pages":7,"known_pages":3},"options":{"exact":9007199254740993}}'
  const local = base.replace('"max_pages":7', '"max_pages":12')
  const remote = base.replace('Before', 'After').replace('"known_pages":3', '"known_pages":9')
  const merged = rebaseProviderJSON(base, local, remote)
  assert.deepEqual(merged.conflicts, [])
  assert.equal(merged.json, remote.replace('"max_pages":7', '"max_pages":12'))
})

test('conflicting values, removed objects and array edits require explicit review', () => {
  for (const [base, local, remote] of [
    ['{"schedule":{"every":"1h"}}', '{"schedule":{"every":"2h"}}', '{"schedule":{"every":"3h"}}'],
    ['{"auth":{"type":"bearer"}}', '{}', '{"auth":{"type":"query"}}'],
    ['{"items":[1,2]}', '{"items":[1,3]}', '{"items":[0,1,2]}'],
    ['{"n":9007199254740992}', '{"n":9007199254740993}', '{"n":9007199254740994}'],
  ])
    assert.equal(rebaseProviderJSON(base, local, remote).conflicts.length, 1)
})

test('independent additions and deletions merge while format-only edits preserve server data', () => {
  const merged = rebaseProviderJSON('{"obsolete":1}', '{ "local": 2 }', '{"obsolete":1,"remote":3}')
  assert.deepEqual(merged.conflicts, [])
  assert.deepEqual(JSON.parse(merged.json), { local: 2, remote: 3 })
  assert.equal(rebaseProviderJSON('{"n":1}', '{ "n" : 1 }', '{"n":2}').json, '{"n":2}')
})

test('invalid source versions cannot be merged into a valid local draft', () => {
  for (const versions of [
    ['{invalid', '{"name":"local"}', '{"name":"remote"}'],
    ['{}', '{"duplicate":1,"duplicate":2}', '{}'],
    ['{}', '{"name":"local"}', '[]'],
  ])
    assert.throws(() => rebaseProviderJSON(...(versions as [string, string, string])))
})
