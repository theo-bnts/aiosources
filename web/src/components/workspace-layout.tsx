import { Suspense, useState } from 'react'
import type { Icon } from '@phosphor-icons/react'
import { Outlet, useLocation } from 'react-router-dom'
import { usePageTabNavigate } from '@/lib/page-tabs'
import { prefetchRoute } from '@/lib/route-prefetch'
import { Loading, PageHeaderVisibility } from '@/components/common'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { PageActionsTarget } from '@/components/page-actions'

type WorkspaceTab = { path: string; matches?: readonly string[]; label: string; icon: Icon }

export function WorkspaceLayout({ title, tabs }: { title: string; tabs: WorkspaceTab[] }) {
  const location = useLocation()
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  const navigate = usePageTabNavigate()
  const current =
    tabs.find(
      (tab) => tab.path === location.pathname || tab.matches?.includes(location.pathname),
    ) ??
    [...tabs]
      .reverse()
      .find(
        (tab) => location.pathname === tab.path || location.pathname.startsWith(tab.path + '/'),
      ) ??
    tabs[0]
  const rootView =
    current.path === location.pathname || Boolean(current.matches?.includes(location.pathname))
  return (
    <div className="page">
      <Tabs
        className="gap-5"
        value={current.path}
        onValueChange={(value) => {
          if (value !== current.path) navigate(value)
        }}
      >
        <div className="flex min-w-0 items-start gap-3" data-workspace-toolbar>
          <TabsList aria-label={`${title} views`} containerClassName="flex-1">
            {tabs.map((tab) => (
              <TabsTrigger
                key={tab.path}
                value={tab.path}
                className="shrink-0"
                onMouseEnter={() => prefetchRoute(tab.path)}
                onFocus={() => prefetchRoute(tab.path)}
                onPointerDown={() => prefetchRoute(tab.path)}
              >
                <tab.icon aria-hidden="true" />
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
          <div ref={setActions} className="toolbar shrink-0 empty:hidden" />
        </div>
        {tabs.map((tab) => (
          <TabsContent key={tab.path} value={tab.path} className="min-w-0 [&_.page]:p-0">
            <PageActionsTarget.Provider value={rootView ? actions : null}>
              <PageHeaderVisibility.Provider value={{ title: !rootView, description: !rootView }}>
                {tab === current && (
                  <Suspense fallback={<Loading label="Loading view" />}>
                    <Outlet />
                  </Suspense>
                )}
              </PageHeaderVisibility.Provider>
            </PageActionsTarget.Provider>
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}
