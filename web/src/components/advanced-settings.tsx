import { useEffect, useRef, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { revealSettingParents } from '@/lib/settings-navigation'
import { Disclosure } from '@/components/ui/disclosure'

// Independent forms and actions belong in sibling disclosures. This container
// only groups them; it never owns a Save button or unmounts a folded draft.
export function AdvancedSettings({ children }: { children: ReactNode }) {
  const { hash, key } = useLocation()
  const container = useRef<HTMLDetailsElement>(null)
  useEffect(() => {
    // Preserve existing links such as #maintenance after nesting their section.
    // Settings-search links have their own field-level navigation.
    const anchor = /^#([a-z][a-z0-9-]*)$/.exec(hash)?.[1]
    const target = anchor ? document.getElementById(anchor) : null
    if (!target || !container.current?.contains(target)) return
    revealSettingParents(target)
    if (target instanceof HTMLDetailsElement && !target.open)
      target.querySelector<HTMLElement>(':scope > summary')?.click()
    const frame = requestAnimationFrame(() => {
      target.scrollIntoView({ block: 'start' })
      target.querySelector<HTMLElement>(':scope > summary')?.focus({ preventScroll: true })
    })
    return () => cancelAnimationFrame(frame)
  }, [hash, key])
  return (
    <Disclosure ref={container} title="Advanced">
      {children}
    </Disclosure>
  )
}
