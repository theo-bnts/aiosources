const previousNavigationKey = 'ingest.navigation.groups'
const currentNavigationKey = 'aiosources.navigation.groups'

type PreferenceStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

export function readNavigationPreferences(storage: PreferenceStorage, key: string): unknown {
  const current = storage.getItem(key)
  if (current !== null) return JSON.parse(current)
  if (key !== currentNavigationKey) return undefined
  const previous = storage.getItem(previousNavigationKey)
  if (previous === null) return undefined
  const parsed: unknown = JSON.parse(previous)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined
  const preferences = Object.fromEntries(
    Object.entries(parsed).filter(([, value]) => typeof value === 'boolean'),
  )
  try {
    storage.setItem(key, JSON.stringify(preferences))
    storage.removeItem(previousNavigationKey)
  } catch {
    // Limited browser storage must not discard the existing preferences.
  }
  return preferences
}
