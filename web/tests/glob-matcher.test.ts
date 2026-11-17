import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { createRequire } from 'node:module'
import test from 'node:test'

const require = createRequire(import.meta.url)
const entries = [
  ...new Set(
    ['vite', 'tinyglobby', 'fdir'].map((name) =>
      createRequire(require.resolve(name)).resolve('picomatch'),
    ),
  ),
]

test('build glob matchers retain file discovery and ordinary repeated wildcard matches', () => {
  const cases: [string, string[], string[]][] = [
    ['src/**/*.{ts,tsx}', ['src/app.tsx', 'src/lib/api.ts'], ['src/style.css', 'other/app.tsx']],
    ['**/*.{css,png,svg}', ['public/logo.svg', 'src/style.css'], ['src/app.tsx']],
    ['+(*)c', ['abc', 'c'], ['ab!']],
    ['+(*a)', ['a', 'aba', 'aaa'], ['b', 'aaa!']],
    ['+(a*)', ['a', 'aba'], ['b']],
    ['+(a*b)', ['ab', 'abab', 'acbab'], ['a', 'aba']],
    ['+(a*b*c)', ['abc', 'abcabc', 'axbyc'], ['ab', 'abc!']],
    ['*(a*b)c', ['c', 'abc', 'ababc'], ['abb']],
    ['x+(*a)y', ['xay', 'xabaay'], ['xby']],
    ['+(\\*a)', ['*a*a'], ['aaa']],
    ['+(foo|bar)', ['foobar', 'foo'], ['other']],
  ]
  for (const entry of entries) {
    const matcher = require(entry)
    for (const [pattern, yes, no] of cases) {
      for (const input of yes)
        assert.equal(matcher.isMatch(input, pattern), true, `${pattern}: ${input}`)
      for (const input of no)
        assert.equal(matcher.isMatch(input, pattern), false, `${pattern}: ${input}`)
    }
  }
})

test('single-branch wildcard repetition finishes within an isolated process deadline', () => {
  // Keep adversarial regex evaluation out of the test runner so a regression
  // fails within a fixed deadline instead of hanging development or CI.
  const script = `
    for (const entry of JSON.parse(process.argv[1])) {
      const matcher = require(entry)
      for (const pattern of ['+(*)a', '+(*a)', '*(a*)b', '+(a*b)c']) {
        if (matcher.isMatch('a'.repeat(10000) + '!', pattern)) throw new Error('Unexpected glob match')
      }
    }
  `
  const result = spawnSync(
    process.execPath,
    ['--input-type=commonjs', '-e', script, JSON.stringify(entries)],
    {
      timeout: 2000,
      killSignal: 'SIGKILL',
      encoding: 'utf8',
      maxBuffer: 64 * 1024,
    },
  )
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stderr)
})
