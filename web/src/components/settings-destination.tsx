import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { useNotification } from '@/lib/notifications'
import {
  useSettingDestination,
  revealSettingParents,
  highlightSetting,
} from '@/lib/settings-navigation'
import { normalizeSettingQuery } from '@/lib/settings-catalog'

function findControl(root: HTMLElement, label: string) {
  const name = normalizeSettingQuery(label)
  for (const element of root.querySelectorAll<HTMLElement>(
    'label, [aria-label], h2, h3, h4, legend, button, summary',
  )) {
    if (
      normalizeSettingQuery(
        element.getAttribute('aria-label') ??
          (element.tagName === 'SUMMARY'
            ? element.parentElement?.getAttribute('data-settings-section')
            : undefined) ??
          element.textContent ??
          '',
      ) !== name
    )
      continue
    if (element instanceof HTMLLabelElement) {
      if (element.control instanceof HTMLElement) return element.control
      return (
        element.querySelector<HTMLElement>('input, select, textarea, button') ??
        element.closest('.field')?.querySelector<HTMLElement>('[contenteditable="true"]') ??
        element
      )
    }
    return element
  }
}

export function SettingsDestination() {
  const location = useLocation()
  const setting = useSettingDestination()
  const client = useQueryClient()
  const notify = useNotification()
  useEffect(() => {
    if (!setting || setting.public === 'interface') return
    let frame = 0
    let finished = false
    let explainedFallback = false
    let clearHighlight: (() => void) | undefined
    const start = performance.now()
    const entity = new URLSearchParams(location.search).get('configure')
    const opened = new WeakSet<HTMLElement>()
    const scan = () => {
      frame = 0
      if (finished || document.querySelector('[data-settings-search]')) return
      const root =
        setting.entity &&
        ['stremio', 'torznab', 'share', 'proxy', 'remote'].includes(setting.entity)
          ? document.querySelector<HTMLElement>(`[data-settings-editor="${setting.entity}"]`)
          : document.getElementById('main-content')
      if (!root) return
      if (root.dataset.settingsEditor && entity && root.dataset.settingsEntity !== entity) return
      for (const summary of root.querySelectorAll<HTMLElement>('details:not([open]) > summary')) {
        const title = summary.textContent?.trim()
        if (title && setting.reveal?.includes(title) && !opened.has(summary)) {
          opened.add(summary)
          revealSettingParents(summary.parentElement!)
          summary.click()
        }
      }
      let control = setting.control ? document.getElementById(setting.control) : undefined
      if (control && !root.contains(control)) control = undefined
      if (!control && setting.entity === 'tracker' && entity) {
        const container = document.getElementById(`tracker-${entity}`)
        if (container) control = findControl(container, setting.label ?? setting.title)
      } else if (!control && setting.entity === 'source-route' && entity) {
        control = document.getElementById(`network-source-${entity}`) ?? undefined
      } else if (!control) control = findControl(root, setting.label ?? setting.title)
      if (
        !control &&
        setting.fallback &&
        root.querySelector('[data-settings-editor="source"]') &&
        !client.isFetching()
      ) {
        const section = [...root.querySelectorAll<HTMLElement>('[data-settings-section]')].find(
          (section) => section.dataset.settingsSection === setting.fallback,
        )
        control = section?.querySelector<HTMLElement>(':scope > summary, h3')
        if (control && !explainedFallback) {
          explainedFallback = true
          notify({
            message: `${setting.title} depends on this source’s configuration. Its configuration section is open.`,
            warning: true,
          })
        }
      }
      if (!control) return
      // A section destination reveals its content as well as its parent sections.
      if (control instanceof HTMLDetailsElement && !control.open) {
        control.querySelector<HTMLElement>(':scope > summary')?.click()
        return
      }
      if (
        control.tagName === 'SUMMARY' &&
        control.parentElement instanceof HTMLDetailsElement &&
        !control.parentElement.open
      ) {
        control.click()
        return
      }
      if (revealSettingParents(control)) return
      if (!control.getClientRects().length) return
      // Let dialog autofocus and AppShell navigation focus finish first.
      finished = true
      observer.disconnect()
      frame = requestAnimationFrame(() => {
        if (control?.isConnected) {
          const disabled = control.matches(':disabled')
          clearHighlight = highlightSetting(
            disabled
              ? (control.closest<HTMLElement>('.field') ?? control.parentElement ?? control)
              : control,
          )
        }
      })
    }
    const schedule = () => {
      if (!frame && !finished) frame = requestAnimationFrame(scan)
    }
    const observer = new MutationObserver(schedule)
    observer.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['open', 'data-state', 'disabled'],
    })
    schedule()
    // No polling. A bounded observer follows lazy pages and their initial requests.
    const timeout = window.setTimeout(() => {
      observer.disconnect()
      if (!finished && performance.now() - start >= 14900) {
        const root = document.getElementById('main-content')
        const fallback = root?.querySelector<HTMLElement>('[role="tabpanel"]')
        if (fallback) clearHighlight = highlightSetting(fallback)
        notify({
          message: client.isFetching()
            ? 'This setting is still loading. Try the search again when the page is ready.'
            : 'This setting is not available in the current configuration. Review the selected source or connection.',
          warning: true,
        })
      }
    }, 15000)
    return () => {
      finished = true
      observer.disconnect()
      cancelAnimationFrame(frame)
      window.clearTimeout(timeout)
      clearHighlight?.()
    }
  }, [setting, location.key, location.search, client, notify])
  return null
}
