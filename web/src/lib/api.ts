import { refreshInterval } from './browser-preferences.ts'
import { QueryClient } from '@tanstack/react-query'

export class ApiError extends Error {
  status: number
  details: string[]
  code?: string
  constructor(status: number, message: string, details: string[] = [], code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.details = details
    this.code = code
  }
}

// Transport failures are distinct from invalid responses and validation errors.
// The console shares service failures in its notification area; field and
// conflict errors remain beside the form that needs attention.
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

export const AUTO_REFRESH_INTERVAL = () => refreshInterval('ordinary')
export const LIVE_REFRESH_INTERVAL = () => refreshInterval('live')
export const COLLECTION_REFRESH_INTERVAL = () => refreshInterval('statistics')

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
    let payload: { error?: string; details?: string[]; code?: string } = {}
    try {
      const value: unknown = await response.json()
      if (value && typeof value === 'object' && !Array.isArray(value)) {
        const fields = value as Record<string, unknown>
        payload = {
          error: typeof fields.error === 'string' ? fields.error : undefined,
          code: typeof fields.code === 'string' ? fields.code : undefined,
          details: Array.isArray(fields.details)
            ? fields.details.filter((item): item is string => typeof item === 'string')
            : [],
        }
      }
    } catch (error) {
      if (error instanceof Error && error.name === 'AbortError') throw error
      /* A proxy may return a non-JSON error. */
    }
    if (response.status === 401 && path !== '/login' && !path.startsWith('/public/')) {
      setCSRF()
      window.dispatchEvent(new Event('session-expired'))
    }
    if (response.status === 403 && !path.startsWith('/public/'))
      void queryClient.invalidateQueries({ queryKey: ['session'] })
    const fallback =
      response.status === 403
        ? 'The security session must be refreshed. Try again after refreshing.'
        : `The request failed (HTTP ${response.status}).`
    throw new ApiError(
      response.status,
      payload.error || fallback,
      payload.details ?? [],
      payload.code,
    )
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
      'torrent-history',
      'torrent-related',
      'run-changes',
      'system-health',
      'aiosource',
      'matching-diagnostics',
      'catalog-search',
      'catalog-title',
      'catalog-releases',
      'stremio-statistics',
      'stremio-requests',
      'security',
      'collection-settings',
      'network-settings',
    ].map((key) =>
      queryClient.invalidateQueries(
        {
          queryKey: [key],
          predicate: (query) => {
            const heavy =
              key === 'source-torrent-stats' ||
              key === 'debrid-statistics' ||
              (key === 'aiosource' &&
                (query.queryKey.length === 1 || query.queryKey[1] === 'information'))
            // Progress events must not turn a slower statistics read into a five-second scan.
            // Explicit writes still refresh immediately; settings and controls stay live.
            return (
              cancelRefetch ||
              !heavy ||
              Date.now() - Math.max(query.state.dataUpdatedAt, query.state.errorUpdatedAt) >=
                COLLECTION_REFRESH_INTERVAL()
            )
          },
        },
        { cancelRefetch },
      ),
    ),
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
