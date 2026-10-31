import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { Loading } from '@/components/common'
import { Disclosure } from '@/components/ui/disclosure'
import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { SettingsSections } from '@/components/settings-sections'

const ImportSettingsPanel = lazy(() =>
  import('@/components/import-settings').then((m) => ({ default: m.ImportSettingsPanel })),
)

export function SourceImportSettings() {
  const location = useLocation()
  const container = useRef<HTMLDetailsElement>(null)
  const [open, setOpen] = useState(false)
  const [visited, setVisited] = useState(false)
  const legacyBookmark =
    location.hash === '#advanced-import-settings' ||
    (!location.hash && new URLSearchParams(location.search).get('settings') === 'advanced')
  useEffect(() => {
    if (open) setVisited(true)
  }, [open])
  useEffect(() => {
    // Older bookmarks still lead to the import controls. Ordinary visits and
    // field-level settings-search links start with every optional section closed.
    if (legacyBookmark) {
      setOpen(true)
      container.current?.scrollIntoView({ block: 'start' })
      container.current?.querySelector('summary')?.focus({ preventScroll: true })
    }
  }, [legacyBookmark])
  return (
    <SettingsSections>
      <Disclosure
        id="import-execution-settings"
        ref={container}
        title="Import execution"
        className="scroll-mt-6"
        open={open}
        onOpenChange={setOpen}
      >
        {(visited || open) && (
          // Folding hides the form without discarding its draft or pending save.
          <Suspense fallback={<Loading label="Loading import settings" />}>
            <ImportSettingsPanel active={open} />
          </Suspense>
        )}
      </Disclosure>
      <RuntimeSettingsPanel section="source_retries" />
    </SettingsSections>
  )
}
