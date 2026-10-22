import { useSyncExternalStore } from 'react'

export type BrowserPreferences = {
  pageSize: 25 | 50 | 100
  dateFormat: 'browser' | 'english' | 'iso'
  timeZone: 'browser' | 'utc'
  refresh: 'normal' | 'reduced'
}

export const defaultBrowserPreferences: BrowserPreferences = {
  pageSize: 50,
  dateFormat: 'browser',
  timeZone: 'browser',
  refresh: 'normal',
}
const storageKey = 'aiosources.interface.preferences'
const listeners = new Set<() => void>()

export function normalizeBrowserPreferences(value: unknown): BrowserPreferences {
  const input = value && typeof value === 'object' ? (value as Partial<BrowserPreferences>) : {}
  return {
    pageSize: input.pageSize === 25 || input.pageSize === 100 ? input.pageSize : 50,
    dateFormat:
      input.dateFormat === 'english' || input.dateFormat === 'iso' ? input.dateFormat : 'browser',
    timeZone: input.timeZone === 'utc' ? 'utc' : 'browser',
    refresh: input.refresh === 'reduced' ? 'reduced' : 'normal',
  }
}
function read(): BrowserPreferences {
  if (typeof window === 'undefined') return { ...defaultBrowserPreferences }
  try {
    return normalizeBrowserPreferences(JSON.parse(localStorage.getItem(storageKey) ?? 'null'))
  } catch {
    return { ...defaultBrowserPreferences }
  }
}
let current = read()
export function browserPreferences() {
  return current
}
export function saveBrowserPreferences(value: BrowserPreferences) {
  current = normalizeBrowserPreferences(value)
  try {
    if (typeof window !== 'undefined') localStorage.setItem(storageKey, JSON.stringify(current))
  } catch {
    /* Keep usable for this visit if storage is blocked. */
  }
  listeners.forEach((listener) => listener())
}
if (typeof window !== 'undefined')
  window.addEventListener('storage', (event) => {
    if (event.key === storageKey || event.key === null) {
      current = read()
      listeners.forEach((listener) => listener())
    }
  })
function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
export function useBrowserPreferences() {
  return useSyncExternalStore(subscribe, browserPreferences, () => defaultBrowserPreferences)
}
export function refreshInterval(kind: 'ordinary' | 'live' | 'statistics') {
  const base = { ordinary: 30_000, live: 5_000, statistics: 15_000 }[kind]
  return current.refresh === 'reduced' ? base * 3 : base
}
