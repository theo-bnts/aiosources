import { useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import { settingFromHash } from './settings-catalog'
import { isSourceSettingsPath } from './source-navigation'
import { settingsEditorDestination, settingsEditorNavigationKey } from './settings-editor'

export function useSettingDestination() {
  const location = useLocation()
  const setting = settingFromHash(location.hash)
  if (!setting) return undefined
  if (setting.public === 'interface') return setting
  const path = setting.route.split('?')[0]
  if (setting.entity === 'source')
    return isSourceSettingsPath(location.pathname) ? setting : undefined
  return path === location.pathname ? setting : undefined
}

// Opening a form is a read-only navigation operation. The lookup is restricted to
// entries returned by that page; arbitrary URL values cannot create or save data.
export function useSettingsEditor<T extends { id: string }>(
  entity: 'remote' | 'stremio' | 'torznab' | 'share' | 'proxy',
  items: readonly T[] | undefined,
  open: (item: T | 'new') => void,
  close: () => void,
  currentSelection: string | null,
) {
  const setting = useSettingDestination()
  const location = useLocation()
  const handled = useRef<{ key: string; editor: boolean } | null>(null)
  const navigationKey = settingsEditorNavigationKey(location)
  const callback = useRef(open)
  callback.current = open
  const closeCallback = useRef(close)
  closeCallback.current = close
  useEffect(() => {
    if (handled.current?.key === navigationKey) return
    if (setting?.public === 'interface') {
      handled.current = { key: navigationKey, editor: handled.current?.editor ?? false }
      return
    }
    const destination = settingsEditorDestination(location)
    if (destination?.entity !== entity) {
      const closeEditor = handled.current?.editor || location.state?.settingsNavigation
      handled.current = { key: navigationKey, editor: false }
      if (closeEditor) closeCallback.current()
      return
    }
    if (!items) return
    const selected = destination.id
    const item = selected === 'new' ? 'new' : items.find((item) => item.id === selected)
    handled.current = { key: navigationKey, editor: true }
    // Moving to another field must preserve a new connection's temporary ID,
    // saved revision and draft instead of mounting a fresh form.
    if (item && selected !== currentSelection) callback.current(item)
  }, [items, setting, entity, navigationKey, location, currentSelection])
}

export function revealSettingParents(element: HTMLElement) {
  const parents: HTMLDetailsElement[] = []
  for (let parent = element.parentElement; parent; parent = parent.parentElement)
    if (parent instanceof HTMLDetailsElement && !parent.open) parents.unshift(parent)
  for (const parent of parents) {
    // Use the existing controlled toggle, preserving lazy loads and form drafts.
    parent.querySelector<HTMLElement>(':scope > summary')?.click()
  }
  return parents.length > 0
}

export function highlightSetting(element: HTMLElement) {
  // Disabled controls stay disabled: navigation highlights their labeled field
  // without changing a filter or attempting to focus an inert native input.
  if (element.matches('input:disabled, select:disabled, textarea:disabled, button:disabled'))
    element = element.closest<HTMLElement>('.field') ?? element.parentElement ?? element
  element.scrollIntoView({ block: 'center', inline: 'nearest', behavior: 'instant' })
  const focusable = element.matches(
    'input:not(:disabled), select:not(:disabled), textarea:not(:disabled), button:not(:disabled), [tabindex]',
  )
  if (!focusable) {
    element.setAttribute('tabindex', '-1')
    element.addEventListener('blur', () => element.removeAttribute('tabindex'), { once: true })
  }
  element.focus({ preventScroll: true })
  element.setAttribute('data-setting-highlight', '')
  const timeout = window.setTimeout(() => element.removeAttribute('data-setting-highlight'), 4000)
  return () => {
    window.clearTimeout(timeout)
    element.removeAttribute('data-setting-highlight')
  }
}
