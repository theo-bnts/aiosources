export type TorznabOptions = {
  sources?: string[]
  media?: string[]
  services?: string[]
  cached_only?: boolean
}

export function torznabPreferenceKey(key: string, options: TorznabOptions): string {
  const base = key.split('~')[0]
  if (!base) return ''
  const value = {
    ...(options.sources?.length ? { sources: options.sources } : {}),
    ...(options.media?.length ? { media: options.media } : {}),
    ...(options.services?.length ? { services: options.services } : {}),
    ...(options.cached_only ? { cached_only: true } : {}),
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
