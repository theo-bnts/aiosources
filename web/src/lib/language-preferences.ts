import { useState } from 'react'

export type LanguagePreferences = {
  language: string
  mode: 'all' | 'preferred' | 'multi' | 'unknown'
}
const key = 'aiosources.language-preferences'
export function browserLanguage() {
  return (navigator.languages?.[0] || navigator.language || 'en').split(/[-_]/)[0].toLowerCase()
}
export function resolvedLanguage(value: LanguagePreferences) {
  return value.language || browserLanguage()
}
export function useLanguagePreferences() {
  const [value, setValue] = useState<LanguagePreferences>(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(key) || 'null')
      if (
        saved &&
        ['all', 'preferred', 'multi', 'unknown'].includes(saved.mode) &&
        typeof saved.language === 'string' &&
        /^[a-z]{0,3}$/.test(saved.language)
      )
        return saved
    } catch {
      /* Preferences are optional; blocked storage does not prevent use. */
    }
    return { language: '', mode: 'unknown' }
  })
  function update(next: LanguagePreferences) {
    setValue(next)
    try {
      localStorage.setItem(key, JSON.stringify(next))
    } catch {
      /* Optional browser preference. */
    }
  }
  return [value, update] as const
}
export type InstallationOptions = {
  sources?: string[]
  services?: string[]
  types?: string[]
  cached_only?: boolean
}

// Keep generated links within the same bounded contract as the server.
export function preferenceManifestError(
  manifest: string,
  options: InstallationOptions,
  language?: LanguagePreferences,
) {
  if ((options.sources?.length ?? 0) > 100) return 'Select up to 100 sources, or use All sources.'
  if (
    language &&
    new TextEncoder().encode(
      JSON.stringify({ language: resolvedLanguage(language), mode: language.mode, ...options }),
    ).length > 4096
  )
    return 'This configuration makes the link too long. Select fewer sources or use All sources.'
  const token = manifest.split('/').at(-2) || ''
  const encoded = token.split('~v1~')[1]
  if (token.length > 8192 || (encoded && Math.floor((encoded.length * 3) / 4) > 4096))
    return 'This configuration makes the link too long. Select fewer sources or use All sources.'
  return null
}

export function preferenceManifest(
  path: string,
  value: LanguagePreferences,
  options?: InstallationOptions,
) {
  const base = path.replace(/~[^/]+(?=\/manifest\.json$)/, '')
  if (
    options &&
    (options.sources?.length ||
      options.services?.length ||
      options.types?.length ||
      options.cached_only)
  ) {
    const preferences = { language: resolvedLanguage(value), mode: value.mode, ...options }
    const data = new TextEncoder().encode(JSON.stringify(preferences))
    const encoded = btoa(String.fromCharCode(...data))
      .replaceAll('+', '-')
      .replaceAll('/', '_')
      .replace(/=+$/, '')
    return base.replace(/\/manifest\.json$/, `~v1~${encoded}/manifest.json`)
  }
  return base.replace(
    /(?:~[^/~]+~[^/~]+)?\/manifest\.json$/,
    `~${resolvedLanguage(value)}~${value.mode}/manifest.json`,
  )
}
