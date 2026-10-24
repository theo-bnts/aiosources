import { resultPreferences, type ResultPreferences } from './result-preferences.ts'

export type TorznabOptions = ResultPreferences & {
  sources?: string[]
  media?: string[]
  services?: string[]
  cached_only?: boolean
}

// Omitted options, empty selections and false all mean the same defaults.
// Keep saved drafts and URL configurations in the same canonical form.
export function torznabCachePreferences(
  value: TorznabOptions,
): Pick<TorznabOptions, 'services' | 'cached_only'> & ResultPreferences {
  return {
    ...resultPreferences(value),
    ...(value.services?.length ? { services: value.services } : {}),
    ...(value.cached_only ? { cached_only: true } : {}),
  }
}

export function torznabPreferenceKey(key: string, options: TorznabOptions): string {
  const base = key.split('~')[0]
  if (!base) return ''
  const value = {
    ...(options.sources?.length ? { sources: options.sources } : {}),
    ...(options.media?.length ? { media: options.media } : {}),
    ...torznabCachePreferences(options),
  }
  if (!Object.keys(value).length) return base
  const data = new TextEncoder().encode(JSON.stringify(value))
  const encoded = btoa(Array.from(data, (byte) => String.fromCharCode(byte)).join(''))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
  return `${base}~v1~${encoded}`
}

export function torznabPreferenceError(key: string, options: TorznabOptions): string {
  if (
    (options.sources?.length ?? 0) > 100 ||
    new TextEncoder().encode(JSON.stringify(options)).length > 4096 ||
    torznabPreferenceKey(key, options).length > 6000
  )
    return 'Select fewer sources or shorten optional announce URLs to keep this link within the request limits.'
  return ''
}
