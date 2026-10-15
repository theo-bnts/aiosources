import { QueryClient } from '@tanstack/react-query'

export class ApiError extends Error {
  status: number
  details: string[]
  constructor(status: number, message: string, details: string[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.details = details
  }
}

// Transport failures share one notification; HTTP and validation errors stay
// beside the operation that failed. Invalid JSON is not a connection failure.
export class ConnectionError extends ApiError {
  constructor(message: string) {
    super(0, message)
  }
}

let csrfToken: string | undefined
export function setCSRF(token?: string) {
  csrfToken = token
}

export function retryRead(count: number, error: unknown) {
  if (count >= 1 || (error instanceof Error && error.name === 'AbortError')) return false
  return (
    !(error instanceof ApiError) ||
    error.status === 0 ||
    error.status === 408 ||
    error.status === 429 ||
    error.status >= 500
  )
}

export const AUTO_REFRESH_INTERVAL = 30_000
export const LIVE_REFRESH_INTERVAL = 5_000
export const COLLECTION_REFRESH_INTERVAL = 15_000

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchInterval: AUTO_REFRESH_INTERVAL,
      refetchOnWindowFocus: true,
      retry: retryRead,
    },
    mutations: { retry: false },
  },
})

async function checkedResponse(path: string, init: RequestInit = {}) {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  if (!['GET', 'HEAD'].includes(method) && csrfToken) headers.set('X-CSRF-Token', csrfToken)
  let response: Response
  try {
    response = await fetch(`/api${path}`, { ...init, credentials: 'same-origin', headers })
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw error
    throw new ConnectionError('The server is unreachable. Check your connection and try again.')
  }
  if (!response.ok) {
    let payload: { error?: string; details?: string[] } = {}
    try {
      const value: unknown = await response.json()
      if (value && typeof value === 'object' && !Array.isArray(value)) {
        const fields = value as Record<string, unknown>
        payload = {
          error: typeof fields.error === 'string' ? fields.error : undefined,
          details: Array.isArray(fields.details)
            ? fields.details.filter((item): item is string => typeof item === 'string')
            : [],
        }
      }
    } catch (error) {
      if (error instanceof Error && error.name === 'AbortError') throw error
      /* A proxy may return a non-JSON error. */
    }
    if (response.status === 401 && path !== '/login') {
      setCSRF()
      window.dispatchEvent(new Event('session-expired'))
    }
    if (response.status === 403) void queryClient.invalidateQueries({ queryKey: ['session'] })
    const fallback =
      response.status === 403
        ? 'The security session must be refreshed. Try again after refreshing.'
        : `The request failed (HTTP ${response.status}).`
    throw new ApiError(response.status, payload.error || fallback, payload.details ?? [])
  }
  return response
}

export async function api<T>(
  path: string,
  init?: RequestInit,
  decode?: (text: string) => T,
): Promise<T> {
  const response = await checkedResponse(path, init)
  if (response.status === 204) return undefined as T
  const text = await readBody(() => response.text())
  if (decode) return decode(text)
  try {
    return JSON.parse(text) as T
  } catch {
    throw new ApiError(0, 'The server returned an invalid response. Try refreshing the page.')
  }
}

async function readBody<T>(read: () => Promise<T>): Promise<T> {
  try {
    return await read()
  } catch (error) {
    if (error instanceof Error && error.name === 'AbortError') throw error
    throw new ConnectionError(
      'The server response was interrupted. Check your connection and try again.',
    )
  }
}

export async function rawPage(id: string, signal?: AbortSignal) {
  const response = await checkedResponse(`/pages/${encodeURIComponent(id)}/download`, { signal })
  const bytes = await readBody(() => response.arrayBuffer())
  return {
    text: new TextDecoder().decode(bytes),
    byteLength: bytes.byteLength,
    contentType: response.headers.get('Content-Type') ?? '',
  }
}

export async function downloadBytes(path: string, filename: string) {
  const response = await checkedResponse(path)
  const url = URL.createObjectURL(await readBody(() => response.blob()))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function params(values: Record<string, string | number | undefined>) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(values))
    if (value !== undefined && value !== '') query.set(key, String(value))
  return query.toString()
}

async function invalidateData(cancelRefetch: boolean) {
  await Promise.all(
    [
      'providers',
      'source-torrent-stats',
      'debrid-statistics',
      'provider',
      'remote-document',
      'schedules',
      'runs',
      'run',
      'operation-history',
      'operation',
      'operation-events',
      'events',
      'raw',
      'raw-record',
      'torrents',
      'secrets',
      'logs',
      'shares',
      'remotes',
      'saved-views',
      'torrent-history',
      'torrent-related',
      'run-changes',
      'system-health',
      'aiosource',
      'matching-diagnostics',
      'stremio-statistics',
      'stremio-requests',
      'security',
      'collection-settings',
    ].map((key) => queryClient.invalidateQueries({ queryKey: [key] }, { cancelRefetch })),
  )
}

// A completed write must supersede reads that started before that write.
export function refreshData() {
  return invalidateData(true)
}

// Repeated live events must let an in-flight read finish, including slow statistics.
export function refreshLiveData() {
  return invalidateData(false)
}
