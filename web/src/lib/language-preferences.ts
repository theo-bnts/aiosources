import { useState } from 'react'
import { resultPreferences, type ResultPreferences } from './result-preferences.ts'

export type LanguagePreferences = {
  language: string
  mode: 'all' | 'preferred' | 'multi' | 'unknown'
}
const key = 'aiosources.language-preferences'
export function defaultLanguagePreferences(): LanguagePreferences {
  return { language: '', mode: 'all' }
}
export function browserLanguage() {
  return (navigator.languages?.[0] || navigator.language || 'en').split(/[-_]/)[0].toLowerCase()
}
export function languageChoices(selected: string[] = []) {
  const names = new Intl.DisplayNames(['en'], { type: 'language' })
  return [
    ...new Set(
      [
        'ar',
        'bg',
        'cs',
        'da',
        'de',
        'el',
        'en',
        'es',
        'fi',
        'fr',
        'he',
        'hi',
        'hr',
        'hu',
        'id',
        'it',
        'ja',
        'ko',
        'nl',
        'no',
        'pl',
        'pt',
        'ro',
        'ru',
        'sk',
        'sl',
        'sr',
        'sv',
        'th',
        'tr',
        'uk',
        'vi',
        'zh',
        browserLanguage(),
        ...selected,
      ].filter(Boolean),
    ),
  ]
    .map((value) => ({ value, label: names.of(value) || value }))
    .sort((a, b) => a.label.localeCompare(b.label))
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
    return defaultLanguagePreferences()
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
export type InstallationOptions = ResultPreferences & {
  sources?: string[]
  services?: string[]
  types?: string[]
  cached_only?: boolean
  max_cached?: number
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
      options.cached_only ||
      options.max_cached ||
      Object.keys(resultPreferences(options)).length)
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
