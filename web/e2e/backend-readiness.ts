import type { ChildProcess } from 'node:child_process'

class BackendBindError extends Error {
  constructor() {
    super('The isolated backend could not bind its reserved HTTP address.')
  }
}

type StartedBackend = { child: ChildProcess; origin: string }

async function ready({ child, origin }: StartedBackend) {
  await new Promise<void>((resolveReady, reject) => {
    let output = ''
    let errors = ''
    const finish = (error?: Error) => {
      clearTimeout(timer)
      child.stdout?.off('data', onData)
      child.stderr?.off('data', onErrors)
      child.off('close', onExit)
      child.off('error', onError)
      if (error) reject(error)
      else resolveReady()
    }
    const onData = (data: Buffer) => {
      output = (output + data.toString()).slice(-8192)
      if (output.includes(`AIOSources ready at ${origin} (version `)) finish()
    }
    // Inspect only in memory for the application's finite bind error. Never put
    // arbitrary startup output, URLs or credentials in a reporter or artifact.
    const onErrors = (data: Buffer) => {
      errors = (errors + data.toString()).slice(-8192)
    }
    const onExit = (code: number | null, signal: NodeJS.Signals | null) =>
      finish(
        !signal && code !== 0 && errors.includes('could not bind the requested HTTP address')
          ? new BackendBindError()
          : new Error(
              'The isolated backend exited before readiness. Build the embedded production binary and check the disposable PostgreSQL service.',
            ),
      )
    const onError = () => finish(new Error('Could not launch E2E_BINARY.'))
    const timer = setTimeout(
      () => finish(new Error('The isolated backend did not become ready within 45 seconds.')),
      45_000,
    )
    child.stdout?.on('data', onData)
    child.stderr?.on('data', onErrors)
    child.once('close', onExit)
    child.once('error', onError)
  })
  // Do not probe any address until this exact owned process confirms its bind.
  const response = await fetch(`${origin}/healthz`, { signal: AbortSignal.timeout(5000) })
  if (!response.ok) throw new Error('The isolated backend failed its readiness check.')
}

export async function launchBackend(start: () => Promise<StartedBackend>): Promise<StartedBackend> {
  for (let attempt = 0; ; attempt++) {
    const started = await start()
    try {
      await ready(started)
      started.child.stdout?.resume()
      started.child.stderr?.resume()
      return started
    } catch (error) {
      // Reserving a port and passing it to a separate process is not atomic.
      // Retry only a confirmed bind conflict from a process that has exited;
      // application/database failures and readiness errors must still fail CI.
      if (!(error instanceof BackendBindError) || attempt >= 2) throw error
    }
  }
}
