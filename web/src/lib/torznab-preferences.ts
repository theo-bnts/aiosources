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
    options.max_results !== undefined &&
    (!Number.isInteger(options.max_results) ||
      options.max_results < 0 ||
      options.max_results > 10000)
  )
    return 'Enter a maximum result count between 1 and 10,000.'
  if (
    options.max_cached !== undefined &&
    (!Number.isInteger(options.max_cached) || options.max_cached < 0 || options.max_cached > 10000)
  )
    return 'Enter a maximum cached result count between 1 and 10,000.'
  if (
    options.response_seconds !== undefined &&
    (!Number.isInteger(options.response_seconds) ||
      options.response_seconds < 0 ||
      options.response_seconds > 600)
  )
    return 'Enter a response timeout between 1 and 600 seconds.'
  if (
    (options.sources?.length ?? 0) > 100 ||
    new TextEncoder().encode(JSON.stringify(options)).length > 4096 ||
    torznabPreferenceKey(key, options).length > 6000
  )
    return 'Select fewer sources or shorten optional announce URLs to keep this link within the request limits.'
  return ''
}
