import { debridProviders, isDebridService } from './debrid'
import { useSyncExternalStore } from 'react'
import { api, ApiError, queryClient } from './api'

export type PublicCredential = {
  credential: string
  services: string[]
  announces?: string[]
  instance_services?: boolean
}
export type PublicInstallation = PublicCredential & { manifest_url: string }
const storageKey = 'aiosources.public.credentials'
const scopedKey = (scope = '') => (scope ? `${storageKey}.${scope}` : storageKey)
const listeners = new Set<() => void>()
const records = new Map<string, PublicCredential | null>()

function parse(value: unknown): PublicCredential | null {
  if (!value || typeof value !== 'object') return null
  const item = value as Record<string, unknown>
  if (
    typeof item.credential !== 'string' ||
    !/^[A-Za-z0-9_-]{32,8192}$/.test(item.credential) ||
    !Array.isArray(item.services) ||
    item.services.length > debridProviders.length ||
    !item.services.every(isDebridService) ||
    (item.announces !== undefined &&
      (!Array.isArray(item.announces) ||
        item.announces.length > 128 ||
        !item.announces.every(
          (id) => typeof id === 'string' && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(id),
        ))) ||
    (!item.services.length &&
      !(item.announces as string[] | undefined)?.length &&
      item.instance_services !== true) ||
    (item.instance_services !== undefined && typeof item.instance_services !== 'boolean')
  )
    return null
  return {
    credential: item.credential,
    ...(item.instance_services === true ? { instance_services: true } : {}),
    services: [...new Set(item.services as string[])],
    announces: [...new Set((item.announces ?? []) as string[])],
  }
}

function read(scope = '') {
  if (!records.has(scope)) {
    let value: PublicCredential | null = null
    try {
      value = parse(JSON.parse(localStorage.getItem(scopedKey(scope)) || 'null'))
    } catch {
      /* Storage is optional. */
    }
    records.set(scope, value)
  }
  return records.get(scope) ?? null
}

function notify() {
  for (const listener of listeners) listener()
}

function changedStorage(event: StorageEvent) {
  if (event.key !== null && !event.key.startsWith(storageKey)) return
  records.clear()
  clearPublicSearch()
  notify()
}

// Credentials can change in another tab while this one shows Administration.
// Keep the store current even when no public form is subscribed to it.
if (typeof window !== 'undefined') window.addEventListener('storage', changedStorage)

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function usePublicCredential(scope = '') {
  return useSyncExternalStore(
    subscribe,
    () => read(scope),
    () => null,
  )
}

export function isCurrentPublicCredential(expected?: string, scope = '') {
  return read(scope)?.credential === expected
}

export function savePublicCredential(value: PublicCredential, scope = '') {
  const next = parse(value)
  if (!next) throw new Error('The server returned an invalid service configuration.')
  records.set(scope, next)
  // Only server-sealed credentials and field identifiers are persisted. Personal
  // keys and announce URLs never enter localStorage or query-cache entries in plaintext.
  try {
    localStorage.setItem(scopedKey(scope), JSON.stringify(next))
  } catch {
    /* Keep this tab usable. */
  }
  clearPublicSearch()
  notify()
}

function clearPublicSearch() {
  void queryClient.cancelQueries({ queryKey: ['public-catalog'] })
  queryClient.removeQueries({ queryKey: ['public-catalog'] })
}

export function forgetPublicCredential(expected?: string, scope = '') {
  if (expected && read(scope)?.credential !== expected) return
  records.set(scope, null)
  try {
    localStorage.removeItem(scopedKey(scope))
  } catch {
    /* Clear memory regardless. */
  }
  clearPublicSearch()
  notify()
}

export async function publicAPI<T>(
  path: string,
  configuration: string,
  init: RequestInit = {},
  remembered = configuration,
) {
  const headers = new Headers(init.headers)
  headers.set('Authorization', `Bearer ${configuration}`)
  try {
    return await api<T>(`/public${path}`, { ...init, headers, cache: 'no-store' })
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) forgetPublicCredential(remembered)
    throw error
  }
}
