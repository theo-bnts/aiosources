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
export function preferenceManifest(path: string, value: LanguagePreferences) {
  return path.replace(
    /(?:~[^/~]+~[^/~]+)?\/manifest\.json$/,
    `~${resolvedLanguage(value)}~${value.mode}/manifest.json`,
  )
}
