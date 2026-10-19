import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { test } from 'node:test'
import { launchBackend } from '../e2e/backend-readiness.ts'

function exitedBackend(message: string) {
  return {
    origin: 'http://127.0.0.1:1',
    child: spawn(
      process.execPath,
      ['-e', 'require("node:fs").writeSync(2, process.argv[1]); process.exit(1)', message],
      { env: {}, stdio: ['ignore', 'pipe', 'pipe'] },
    ),
  }
}

test('an isolated backend bind conflict retries a fresh child without retrying application errors', async () => {
  let starts = 0
  const privateText = 'temporary-test-password-must-not-be-reported'
  await assert.rejects(
    launchBackend(async () =>
      exitedBackend(
        starts++ === 0
          ? 'could not bind the requested HTTP address\n'
          : 'database startup failure: ' + privateText,
      ),
    ),
    (error: Error) => {
      assert.match(error.message, /exited before readiness/)
      assert.equal(error.message.includes(privateText), false)
      return true
    },
  )
  assert.equal(starts, 2)
})

test('repeated backend bind conflicts are bounded and never become a readiness success', async () => {
  let starts = 0
  await assert.rejects(
    launchBackend(async () => {
      starts++
      return exitedBackend('could not bind the requested HTTP address\n')
    }),
    /could not bind its reserved HTTP address/,
  )
  assert.equal(starts, 3)
})

test('unrelated backend exits fail immediately without publishing captured startup output', async () => {
  let starts = 0
  await assert.rejects(
    launchBackend(async () => {
      starts++
      return exitedBackend('other startup failure with private diagnostics\n')
    }),
    /exited before readiness/,
  )
  assert.equal(starts, 1)
})
