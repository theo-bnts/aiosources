import { lazy, Suspense } from 'react'
import { CloudArrowDownIcon, ShareNetworkIcon } from '@phosphor-icons/react'
import { useLocation } from 'react-router-dom'
import { usePageTabNavigate } from '@/lib/page-tabs'
import { Loading } from '@/components/common'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const SharedCatalogs = lazy(() => import('@/pages/sharing'))
const RemoteCatalogs = lazy(() => import('@/pages/remotes'))

export default function CatalogSharingPage() {
  const location = useLocation()
  const navigate = usePageTabNavigate()
  const tab = location.pathname === '/sharing/remotes' ? 'remote' : 'shared'
  return (
    <div className="page">
      <Tabs
        className="gap-5"
        value={tab}
        onValueChange={(value) => {
          if (value !== tab)
            navigate((value === 'remote' ? '/sharing/remotes' : '/sharing') + location.search)
        }}
      >
        <TabsList
          aria-label="Catalog sharing views"
          className="max-w-full justify-start overflow-x-auto"
        >
          <TabsTrigger value="shared" className="shrink-0">
            <ShareNetworkIcon aria-hidden="true" />
            Shared catalogs
          </TabsTrigger>
          <TabsTrigger value="remote" className="shrink-0">
            <CloudArrowDownIcon aria-hidden="true" />
            Remote catalogs
          </TabsTrigger>
        </TabsList>
        <TabsContent value="shared" className="min-w-0">
          <Suspense fallback={<Loading label="Loading shared catalogs" />}>
            <SharedCatalogs />
          </Suspense>
        </TabsContent>
        <TabsContent value="remote" className="min-w-0">
          <Suspense fallback={<Loading label="Loading remote catalogs" />}>
            <RemoteCatalogs />
          </Suspense>
        </TabsContent>
      </Tabs>
    </div>
  )
}
