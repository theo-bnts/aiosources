import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { Loading } from '@/components/common'
import { Disclosure } from '@/components/ui/disclosure'
import { RuntimeSettingsPanel } from '@/components/runtime-settings'

const ImportSettingsPanel = lazy(() =>
  import('@/components/import-settings').then((m) => ({ default: m.ImportSettingsPanel })),
)

export function AdvancedImportSettings() {
  const [search, setSearch] = useViewSearchParams()
  const location = useLocation()
  const container = useRef<HTMLDetailsElement>(null)
  const open = search.get('settings') === 'advanced'
  const [visited, setVisited] = useState(open)
  const [executionOpen, setExecutionOpen] = useState(false)
  useEffect(() => {
    if (open) setVisited(true)
    if (open && location.hash === '#advanced-import-settings') {
      container.current?.scrollIntoView({ block: 'start' })
      container.current?.querySelector('summary')?.focus({ preventScroll: true })
    }
  }, [open, location.hash])
  return (
    <Disclosure
      id="advanced-import-settings"
      ref={container}
      title="Advanced"
      className="scroll-mt-6"
      open={open}
      onOpenChange={(expanded) => {
        const next = new URLSearchParams(search)
        if (expanded) next.set('settings', 'advanced')
        else next.delete('settings')
        setSearch(next, { replace: true })
      }}
    >
      {(visited || open) && (
        // Folding only hides the form. Keep its draft and pending save mounted;
        // ordinary navigation retains the shared unsaved-changes guard.
        <Suspense fallback={<Loading label="Loading import settings" />}>
          <Disclosure title="Import execution" open={executionOpen} onOpenChange={setExecutionOpen}>
            <ImportSettingsPanel active={open && executionOpen} />
          </Disclosure>
          <RuntimeSettingsPanel section="source_retries" />
        </Suspense>
      )}
    </Disclosure>
  )
}
