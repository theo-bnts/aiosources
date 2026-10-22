import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { QueryObserver } from '@tanstack/react-query'
import {
  api,
  ApiError,
  downloadBytes,
  rawPage,
  queryClient,
  refreshData,
  refreshLiveData,
  retryRead,
  setCSRF,
} from '../src/lib/api.ts'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
  queryClient.clear()
  setCSRF()
})

test('live updates let a slow active read finish without cancelling or replaying it', async () => {
  let calls = 0
  let cancelled = 0
  const finish: Array<(value: string) => void> = []
  queryClient.setQueryData(['torrents'], 'saved')
  const observer = new QueryObserver(queryClient, {
    queryKey: ['torrents'],
    staleTime: Infinity,
    queryFn: ({ signal }) => {
      calls++
      signal.addEventListener('abort', () => cancelled++, { once: true })
      return new Promise<string>((resolve) => finish.push(resolve))
    },
  })
  const unsubscribe = observer.subscribe(() => {})
  try {
    const read = observer.refetch()
    const events = Promise.all(Array.from({ length: 5 }, () => refreshLiveData()))
    await new Promise((resolve) => setImmediate(resolve))
    for (const resolve of finish) resolve('received')
    await Promise.all([read, events])
    assert.equal(cancelled, 0)
    assert.equal(calls, 1)
    assert.equal(observer.getCurrentResult().data, 'received')
  } finally {
    for (const resolve of finish) resolve('cleanup')
    unsubscribe()
  }
})

test('a completed write supersedes an older read instead of displaying its stale snapshot', async () => {
  let snapshot = 'before write'
  let cancelled = 0
  const finish: Array<() => void> = []
  queryClient.setQueryData(['providers'], 'saved')
  const observer = new QueryObserver(queryClient, {
    queryKey: ['providers'],
    staleTime: Infinity,
    queryFn: ({ signal }) => {
      const value = snapshot
      signal.addEventListener('abort', () => cancelled++, { once: true })
      return new Promise<string>((resolve) => finish.push(() => resolve(value)))
    },
  })
  const unsubscribe = observer.subscribe(() => {})
  try {
    const read = observer.refetch()
    snapshot = 'after write'
    const updated = refreshData()
    await new Promise((resolve) => setImmediate(resolve))
    for (const resolve of finish) resolve()
    await Promise.all([read, updated])
    assert.equal(cancelled, 1)
    assert.equal(observer.getCurrentResult().data, 'after write')
  } finally {
    for (const resolve of finish) resolve()
    unsubscribe()
  }
})

test('every read uses the same bounded retry policy without replaying client errors or cancellations', () => {
  for (const status of [0, 408, 429, 500, 502, 503, 504]) {
    assert.equal(retryRead(0, new ApiError(status, 'synthetic')), true)
    assert.equal(retryRead(1, new ApiError(status, 'synthetic')), false)
  }
  for (const status of [400, 401, 403, 404, 409, 422])
    assert.equal(retryRead(0, new ApiError(status, 'synthetic')), false)
  assert.equal(retryRead(0, new DOMException('cancelled', 'AbortError')), false)
})

test('a lost read connection recovers on one retry', async () => {
  let attempts = 0
  globalThis.fetch = async () => {
    if (++attempts === 1) throw new TypeError('synthetic lost connection')
    return Response.json({ retained: true })
  }
  const result = await queryClient.fetchQuery({
    queryKey: ['connection-regression'],
    queryFn: () => api('/connection-regression'),
    retryDelay: 0,
  })
  assert.deepEqual(result, { retained: true })
  assert.equal(attempts, 2)
})

test('an ambiguous write failure is never submitted twice automatically', async () => {
  let attempts = 0
  globalThis.fetch = async () => {
    attempts++
    throw new TypeError('synthetic lost acknowledgement')
  }
  const mutation = queryClient.getMutationCache().build(queryClient, {
    mutationFn: () => api('/write-regression', { method: 'POST', body: '{}' }),
  })
  await assert.rejects(mutation.execute(undefined), (error: unknown) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 0)
    return true
  })
  assert.equal(attempts, 1)
})

test('proxy error payloads cannot break error rendering or leak parse diagnostics', async () => {
  for (const body of ['null', '[]', '42', '"proxy"', '<html>synthetic private payload</html>']) {
    globalThis.fetch = async () => new Response(body, { status: 502 })
    await assert.rejects(api('/proxy-regression'), (error: unknown) => {
      assert.ok(error instanceof ApiError)
      assert.equal(error.status, 502)
      assert.match(error.message, /HTTP 502/)
      assert.deepEqual(error.details, [])
      assert.ok(!error.message.includes('private payload'))
      return true
    })
  }
  globalThis.fetch = async () =>
    Response.json({ error: 12, details: [null, 42, 'Supported explanation'] }, { status: 422 })
  await assert.rejects(api('/details-regression'), (error: unknown) => {
    assert.ok(error instanceof ApiError)
    assert.deepEqual(error.details, ['Supported explanation'])
    assert.match(error.message, /HTTP 422/)
    return true
  })
})

test('invalid and interrupted success responses use understandable errors', async () => {
  globalThis.fetch = async () => new Response('{"retained":')
  await assert.rejects(api('/invalid-regression'), (error: unknown) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 0)
    assert.match(error.message, /invalid response/)
    return true
  })
  globalThis.fetch = async () =>
    new Response(
      new ReadableStream({
        start(controller) {
          controller.error(new TypeError('synthetic interrupted response'))
        },
      }),
    )
  await assert.rejects(api('/interrupted-regression'), (error: unknown) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.status, 0)
    assert.match(error.message, /interrupted/)
    return true
  })
})

test('exact decoders, no-content writes and CSRF continue to work', async () => {
  const raw = '{ "huge": 123456789012345678901234567890, "zero": 0.000 }'
  globalThis.fetch = async () => new Response(raw)
  assert.equal(await api('/exact-regression', undefined, (text) => text), raw)
  setCSRF('synthetic-form-token')
  globalThis.fetch = async (_input, init) => {
    assert.equal(new Headers(init?.headers).get('X-CSRF-Token'), 'synthetic-form-token')
    assert.equal(new Headers(init?.headers).get('Content-Type'), 'application/json')
    assert.equal(init?.credentials, 'same-origin')
    return new Response(null, { status: 204 })
  }
  assert.equal(await api('/write-regression', { method: 'post', body: '{}' }), undefined)
})

test('JSON, raw pages and downloads share interrupted-read and cancellation handling', async () => {
  for (const request of [
    () => api('/body'),
    () => rawPage('fixture'),
    () => downloadBytes('/body', 'fixture'),
  ]) {
    globalThis.fetch = async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.error(new TypeError('interrupted'))
          },
        }),
      )
    await assert.rejects(request(), (error: unknown) => {
      assert.ok(error instanceof ApiError)
      assert.equal(error.status, 0)
      assert.match(error.message, /interrupted/)
      return true
    })
    const cancelled = new DOMException('cancelled', 'AbortError')
    globalThis.fetch = async () =>
      new Response(
        new ReadableStream({
          start(controller) {
            controller.error(cancelled)
          },
        }),
      )
    await assert.rejects(request(), (error: unknown) => error === cancelled)
  }
  const cancelled = new Error('cancelled')
  cancelled.name = 'AbortError'
  globalThis.fetch = async () => {
    throw cancelled
  }
  await assert.rejects(api('/cancelled-fetch'), (error: unknown) => error === cancelled)
})

test('refresh covers mutable source data and diagnostics without invalidating immutable archives', async () => {
  const keys = [
    'provider',
    'source-torrent-stats',
    'remote-document',
    'raw-record',
    'aiosource',
    'matching-diagnostics',
    'operation-history',
    'operation',
    'operation-events',
  ]
  for (const key of keys) queryClient.setQueryData([key, 'fixture'], { retained: true })
  queryClient.setQueryData(['raw-page', 'fixture'], { text: 'Original archive bytes' })
  await refreshData()
  for (const key of keys)
    assert.equal(queryClient.getQueryState([key, 'fixture'])?.isInvalidated, true, key)
  assert.equal(queryClient.getQueryState(['raw-page', 'fixture'])?.isInvalidated, false)
})

test('frequent progress events honor statistics pacing while controls and explicit writes stay immediate', async () => {
  const originalNow = Date.now
  let now = originalNow()
  Date.now = () => now
  let statistics = 0,
    controls = 0
  const observers = [
    new QueryObserver(queryClient, {
      queryKey: ['aiosource'],
      staleTime: Infinity,
      refetchInterval: false,
      queryFn: async () => ++statistics,
    }),
    new QueryObserver(queryClient, {
      queryKey: ['aiosource', 'control'],
      staleTime: Infinity,
      refetchInterval: false,
      queryFn: async () => ++controls,
    }),
  ]
  queryClient.setQueryData(['aiosource'], 0)
  queryClient.setQueryData(['aiosource', 'control'], 0)
  const unsubscribes = observers.map((observer) => observer.subscribe(() => {}))
  try {
    for (let i = 0; i < 12; i++) {
      now += 5000
      await refreshLiveData()
    }
    assert.equal(statistics, 4)
    assert.equal(controls, 12)
    await refreshData()
    assert.equal(statistics, 5)
    assert.equal(controls, 13)
  } finally {
    unsubscribes.forEach((unsubscribe) => unsubscribe())
    observers.forEach((observer) => observer.destroy())
    Date.now = originalNow
  }
})
