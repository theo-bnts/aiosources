import { debridProviders, isDebridService } from './debrid'
import { useSyncExternalStore } from 'react'
import { api, ApiError, queryClient } from './api'

export type PublicCredential = { credential: string; services: string[]; announces?: string[] }
export type PublicInstallation = PublicCredential & { manifest_url: string }
const storageKey = 'aiosources.public.credentials'
const listeners = new Set<() => void>()
let current: PublicCredential | null = null
let loaded = false

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
    (!item.services.length && !(item.announces as string[] | undefined)?.length)
  )
    return null
  return {
    credential: item.credential,
    services: [...new Set(item.services as string[])],
    announces: [...new Set((item.announces ?? []) as string[])],
  }
}

function read() {
  if (!loaded) {
    loaded = true
    current = null
    try {
      current = parse(JSON.parse(localStorage.getItem(storageKey) || 'null'))
    } catch {
      /* Storage is optional. */
    }
  }
  return current
}

function notify() {
  for (const listener of listeners) listener()
}

function changedStorage(event: StorageEvent) {
  if (event.key !== storageKey && event.key !== null) return
  loaded = false
  clearPublicSearch()
  notify()
}

function subscribe(listener: () => void) {
  if (!listeners.size) window.addEventListener('storage', changedStorage)
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
    if (!listeners.size) window.removeEventListener('storage', changedStorage)
  }
}

export function usePublicCredential() {
  return useSyncExternalStore(subscribe, read, () => null)
}

export function isCurrentPublicCredential(expected?: string) {
  return read()?.credential === expected
}

export function savePublicCredential(value: PublicCredential) {
  const next = parse(value)
  if (!next) throw new Error('The server returned an invalid service configuration.')
  current = next
  loaded = true
  // Only server-sealed credentials and field identifiers are persisted. Personal
  // keys and announce URLs never enter localStorage or query-cache entries in plaintext.
  try {
    localStorage.setItem(storageKey, JSON.stringify(next))
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

export function forgetPublicCredential(expected?: string) {
  if (expected && read()?.credential !== expected) return
  current = null
  loaded = true
  try {
    localStorage.removeItem(storageKey)
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
