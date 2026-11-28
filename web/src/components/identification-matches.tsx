import { Suspense, type ReactNode } from 'react'
import { LinkIcon, ListChecksIcon } from '@phosphor-icons/react'
import { usePageTabNavigate } from '@/lib/page-tabs'
import { prefetchRoute } from '@/lib/route-prefetch'
import {
  identificationCorrespondencesPath,
  identificationResultsPath,
} from '@/lib/metadata-navigation'
import { cn } from '@/lib/utils'
import { Loading } from '@/components/common'
import { tabsListVariants, tabsTriggerClassName } from '@/components/ui/tabs'

export type IdentificationMatchesView = 'results' | 'correspondences'

export function IdentificationMatchesLayout({
  view,
  children,
}: {
  view: IdentificationMatchesView
  children: ReactNode
}) {
  const navigate = usePageTabNavigate()
  return (
    <div className="min-w-0 space-y-5">
      <div className="min-w-0 max-w-full overflow-x-auto overflow-y-hidden">
        <div
          data-slot="tabs-list"
          data-variant="default"
          className={cn(tabsListVariants({ variant: 'default' }), 'sm:w-fit sm:min-w-0')}
          role="group"
          aria-label="Matches view"
        >
          <button
            type="button"
            data-slot="tabs-trigger"
            data-state={view === 'results' ? 'active' : 'inactive'}
            aria-pressed={view === 'results'}
            className={cn(tabsTriggerClassName, 'px-3')}
            onMouseEnter={() => prefetchRoute(identificationResultsPath)}
            onFocus={() => prefetchRoute(identificationResultsPath)}
            onPointerDown={() => prefetchRoute(identificationResultsPath)}
            onClick={() => {
              if (view !== 'results') navigate(identificationResultsPath)
            }}
          >
            <ListChecksIcon aria-hidden="true" />
            Results
          </button>
          <button
            type="button"
            data-slot="tabs-trigger"
            data-state={view === 'correspondences' ? 'active' : 'inactive'}
            aria-pressed={view === 'correspondences'}
            className={cn(tabsTriggerClassName, 'px-3')}
            onMouseEnter={() => prefetchRoute(identificationCorrespondencesPath)}
            onFocus={() => prefetchRoute(identificationCorrespondencesPath)}
            onPointerDown={() => prefetchRoute(identificationCorrespondencesPath)}
            onClick={() => {
              if (view !== 'correspondences') navigate(identificationCorrespondencesPath)
            }}
          >
            <LinkIcon aria-hidden="true" />
            Correspondences
          </button>
        </div>
      </div>
      <div className="min-w-0 [&_.page]:p-0">
        <Suspense fallback={<Loading label="Loading view" />}>{children}</Suspense>
      </div>
    </div>
  )
}
