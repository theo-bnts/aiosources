import { Suspense } from 'react'
import type { Icon } from '@phosphor-icons/react'
import { Outlet, useLocation } from 'react-router-dom'
import { usePageTabNavigate } from '@/lib/page-tabs'
import { Loading, PageHeaderVisibility } from '@/components/common'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type WorkspaceTab = { path: string; label: string; icon: Icon; routes?: string[] }

export function WorkspaceLayout({ title, tabs }: { title: string; tabs: WorkspaceTab[] }) {
  const location = useLocation()
  const navigate = usePageTabNavigate()
  const current =
    tabs.find((tab) => tab.path === location.pathname) ??
    [...tabs]
      .reverse()
      .find((tab) =>
        (tab.routes ?? [tab.path]).some(
          (path) => location.pathname === path || location.pathname.startsWith(path + '/'),
        ),
      ) ??
    tabs[0]
  const rootView = (current.routes ?? [current.path]).includes(location.pathname)
  return (
    <div className="page">
      <Tabs
        className="gap-5"
        value={current.path}
        onValueChange={(value) => {
          if (value !== current.path) navigate(value)
        }}
      >
        <TabsList
          aria-label={`${title} views`}
          className="max-w-full justify-start overflow-x-auto"
        >
          {tabs.map((tab) => (
            <TabsTrigger key={tab.path} value={tab.path} className="shrink-0">
              <tab.icon aria-hidden="true" />
              {tab.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {tabs.map((tab) => (
          <TabsContent key={tab.path} value={tab.path} className="min-w-0 [&_.page]:p-0">
            <PageHeaderVisibility.Provider value={{ title: !rootView, description: !rootView }}>
              {tab === current && (
                <Suspense fallback={<Loading label="Loading view" />}>
                  <Outlet />
                </Suspense>
              )}
            </PageHeaderVisibility.Provider>
          </TabsContent>
        ))}
      </Tabs>
    </div>
  )
}
