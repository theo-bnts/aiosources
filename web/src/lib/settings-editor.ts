import { settingFromHash } from './settings-catalog.ts'

export type SettingsEditorIdentity = {
  entity: 'remote' | 'stremio' | 'torznab' | 'share' | 'proxy'
  id: string
}
type EditorLocation = { pathname: string; search: string; hash: string; state?: unknown }

export function settingsEditorNavigationKey(location: EditorLocation) {
  const state = location.state as { settingsNavigationId?: unknown } | null
  return JSON.stringify([
    location.pathname,
    new URLSearchParams(location.search).get('configure'),
    location.hash,
    typeof state?.settingsNavigationId === 'string' ? state.settingsNavigationId : '',
  ])
}

export function settingsEditorDestination(
  location: EditorLocation,
): SettingsEditorIdentity | undefined {
  const setting = settingFromHash(location.hash)
  const id = new URLSearchParams(location.search).get('configure')
  if (
    !id ||
    !setting?.entity ||
    !['remote', 'stremio', 'torznab', 'share', 'proxy'].includes(setting.entity) ||
    setting.route.split('?')[0] !== location.pathname
  )
    return undefined
  return { entity: setting.entity as SettingsEditorIdentity['entity'], id }
}

// Directory filters do not replace an editor. A different connection or a
// destination outside that editor can, including when reached with Back/Forward.
export function changesSettingsEditor(
  current: EditorLocation,
  next: EditorLocation,
  editor: SettingsEditorIdentity,
) {
  if (settingsEditorNavigationKey(current) === settingsEditorNavigationKey(next)) return false
  if (settingFromHash(next.hash)?.public === 'interface') return false
  const destination = settingsEditorDestination(next)
  return destination?.entity !== editor.entity || destination?.id !== editor.id
}
