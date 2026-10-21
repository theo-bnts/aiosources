import React, { lazy } from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import {
  createBrowserRouter,
  createRoutesFromElements,
  RouterProvider,
  Route,
  Link,
  Outlet,
  Navigate,
  useLocation,
} from 'react-router-dom'
import { UnsavedChangesProvider } from '@/lib/unsaved-changes'
import {
  IconContext,
  ChartBarIcon,
  GearSixIcon,
  ClockCounterClockwiseIcon,
  GlobeIcon,
  PathIcon,
  GaugeIcon,
} from '@phosphor-icons/react'
import { queryClient } from '@/lib/api'
import { AppShell, RootBoundary, SessionGate } from '@/app'
import { Empty } from '@/components/common'
import { WorkspaceLayout } from '@/components/workspace-layout'
import { Button } from '@/components/ui/button'
import { AboutDialog } from '@/components/about-dialog'
import { NotificationProvider } from '@/components/notifications'
import './index.css'

const SourceStatisticsPage = lazy(() =>
  import('@/pages/statistics').then((m) => ({ default: m.SourceStatisticsPage })),
)
const AIOStatisticsPage = lazy(() =>
  import('@/pages/statistics').then((m) => ({ default: m.AIOStatisticsPage })),
)
const ProvidersPage = lazy(() =>
  import('@/pages/providers').then((m) => ({ default: m.ProvidersPage })),
)
const ProviderEditor = lazy(() =>
  import('@/pages/providers').then((m) => ({ default: m.ProviderEditor })),
)
const RunsPage = lazy(() => import('@/pages/runs').then((m) => ({ default: m.RunsPage })))
const RunDetailPage = lazy(() => import('@/pages/runs').then((m) => ({ default: m.RunDetailPage })))
const TorrentsPage = lazy(() =>
  import('@/pages/library').then((m) => ({ default: m.TorrentsPage })),
)
const MatchingDiagnostics = lazy(() =>
  import('@/components/matching-diagnostics').then((m) => ({ default: m.MatchingDiagnostics })),
)
const StremioStatisticsPage = lazy(() =>
  import('@/pages/stremio-history').then((m) => ({ default: m.StremioStatisticsPage })),
)
const StremioHistoryPage = lazy(() =>
  import('@/pages/stremio-history').then((m) => ({ default: m.StremioHistoryPage })),
)
const StremioRequestPage = lazy(() =>
  import('@/pages/stremio-history').then((m) => ({ default: m.StremioRequestPage })),
)
const RawDetailPage = lazy(() =>
  import('@/pages/library').then((m) => ({ default: m.RawDetailPage })),
)
const RawPageView = lazy(() => import('@/pages/library').then((m) => ({ default: m.RawPageView })))
const LogsPage = lazy(() => import('@/pages/logs').then((m) => ({ default: m.LogsPage })))
const NetworkPage = lazy(() => import('@/pages/network').then((m) => ({ default: m.NetworkPage })))
const NetworkRoutingPage = lazy(() =>
  import('@/pages/network').then((m) => ({ default: m.NetworkRoutingPage })),
)
const IncomingNetworkPage = lazy(() => import('@/pages/incoming-network'))
const PublicRequestLimitsPage = lazy(() =>
  import('@/pages/incoming-network').then((m) => ({ default: m.PublicRequestLimitsPage })),
)
const SharingPage = lazy(() => import('@/pages/sharing'))
const RemotesPage = lazy(() => import('@/pages/remotes'))
const RemoteStatisticsPage = lazy(() =>
  import('@/pages/remotes').then((m) => ({ default: m.RemoteStatisticsPage })),
)
const SharedCatalogStatisticsPage = lazy(() =>
  import('@/pages/sharing').then((m) => ({ default: m.SharedCatalogStatisticsPage })),
)
const PublicAccessPage = lazy(() => import('@/pages/public-access'))
const TorznabPage = lazy(() => import('@/pages/torznab'))
const HealthPage = lazy(() => import('@/pages/health').then((m) => ({ default: m.HealthPage })))
const StorageSettingsPage = lazy(() =>
  import('@/pages/health').then((m) => ({ default: m.StorageSettingsPage })),
)
const SecurityStatisticsPage = lazy(() =>
  import('@/pages/security').then((m) => ({ default: m.SecurityStatisticsPage })),
)
const SecuritySettingsPage = lazy(() =>
  import('@/pages/security').then((m) => ({ default: m.SecuritySettingsPage })),
)
function LegacyImportSettings() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  search.set('settings', 'advanced')
  return (
    <Navigate
      to={`/providers/settings?${search}#advanced-import-settings`}
      state={location.state}
      replace
    />
  )
}

function LegacyActivity() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  search.delete('unread')
  search.delete('include_deleted')
  if (location.pathname === '/webhooks') search.set('q', 'external_notification')
  return <Navigate to={`/logs${search.size ? '?' + search : ''}`} state={location.state} replace />
}

function LegacyRemotes() {
  const location = useLocation()
  return (
    <Navigate
      to={`/remotes/settings${location.search}${location.hash}`}
      state={location.state}
      replace
    />
  )
}

function LegacyRawHistory() {
  const location = useLocation()
  const search = new URLSearchParams(location.search)
  const run = search.get('run') || search.get('run_id')
  const target = new URLSearchParams()
  if (run) {
    target.set('tab', 'raw')
    for (const key of ['q', 'status', 'offset', 'limit']) {
      const value = search.get(key)
      if (value) target.set('raw_' + key, value)
    }
  } else if (search.get('provider')) target.set('provider', search.get('provider')!)
  return (
    <Navigate
      to={`${run ? '/runs/' + encodeURIComponent(run) : '/runs'}${target.size ? '?' + target : ''}`}
      replace
      state={location.state}
    />
  )
}

const AIOSourcePage = lazy(() => import('@/pages/aiosource'))
const CatalogSearchPage = lazy(() =>
  import('@/pages/catalog-search').then((m) => ({ default: m.CatalogSearchPage })),
)
const CatalogTitlePage = lazy(() =>
  import('@/pages/catalog-search').then((m) => ({ default: m.CatalogTitlePage })),
)
const OperationHistoryPage = lazy(() =>
  import('@/pages/operation-history').then((m) => ({ default: m.OperationHistoryPage })),
)
const OperationDetailPage = lazy(() =>
  import('@/pages/operation-history').then((m) => ({ default: m.OperationDetailPage })),
)

const router = createBrowserRouter(
  createRoutesFromElements(
    <Route
      element={
        <>
          <div className="fixed top-2.5 right-5 z-30 md:right-9 lg:top-3.5">
            <AboutDialog />
          </div>
          <UnsavedChangesProvider>
            <SessionGate>
              <Outlet />
            </SessionGate>
          </UnsavedChangesProvider>
        </>
      }
      errorElement={
        <div className="page">
          <Empty title="Unable to open this page" description="Reload the page to try again." />
        </div>
      }
    >
      <Route element={<AppShell />}>
        <Route index element={<Navigate to="/providers" replace />} />
        <Route
          element={
            <WorkspaceLayout
              title="Sources"
              tabs={[
                { path: '/providers', label: 'Statistics', icon: ChartBarIcon },
                {
                  path: '/providers/settings',
                  label: 'Settings',
                  icon: GearSixIcon,
                  routes: ['/providers/settings', '/providers'],
                },
                {
                  path: '/runs',
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                  routes: ['/runs', '/raw', '/pages'],
                },
              ]}
            />
          }
        >
          <Route path="providers" element={<SourceStatisticsPage />} />
          <Route path="providers/settings" element={<ProvidersPage />} />
          <Route path="providers/new" element={<ProviderEditor key="new" />} />
          <Route path="providers/:id" element={<ProviderEditor />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="runs/:id" element={<RunDetailPage />} />
          <Route path="raw" element={<LegacyRawHistory />} />
          <Route path="raw/:id" element={<RawDetailPage />} />
          <Route path="pages/:id" element={<RawPageView />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Metadata"
              tabs={[
                { path: '/metadata', label: 'Statistics', icon: ChartBarIcon },
                { path: '/metadata/settings', label: 'Settings', icon: GearSixIcon },
                { path: '/metadata/history', label: 'History', icon: ClockCounterClockwiseIcon },
              ]}
            />
          }
        >
          <Route path="metadata" element={<AIOStatisticsPage section="metadata" />} />
          <Route
            path="metadata/settings"
            element={<AIOSourcePage key="metadata" section="metadata" />}
          />
          <Route
            path="metadata/history"
            element={<OperationHistoryPage key="metadata-history" section="metadata" />}
          />
          <Route path="metadata/history/:id" element={<OperationDetailPage section="metadata" />} />
        </Route>
        <Route path="metadata/identification" element={<MatchingDiagnostics />} />
        <Route
          element={
            <WorkspaceLayout
              title="Debrid"
              tabs={[
                { path: '/debrid', label: 'Statistics', icon: ChartBarIcon },
                { path: '/debrid/settings', label: 'Settings', icon: GearSixIcon },
                { path: '/debrid/history', label: 'History', icon: ClockCounterClockwiseIcon },
              ]}
            />
          }
        >
          <Route path="debrid" element={<AIOStatisticsPage section="debrid" />} />
          <Route path="debrid/settings" element={<AIOSourcePage key="debrid" section="debrid" />} />
          <Route
            path="debrid/history"
            element={<OperationHistoryPage key="debrid-history" section="debrid" />}
          />
          <Route path="debrid/history/:id" element={<OperationDetailPage section="debrid" />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Storage"
              tabs={[
                { path: '/health', label: 'Statistics', icon: ChartBarIcon },
                { path: '/health/settings', label: 'Settings', icon: GearSixIcon },
              ]}
            />
          }
        >
          <Route path="health" element={<HealthPage />} />
          <Route path="health/settings" element={<StorageSettingsPage />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Security"
              tabs={[
                { path: '/security', label: 'Statistics', icon: ChartBarIcon },
                { path: '/security/settings', label: 'Settings', icon: GearSixIcon },
              ]}
            />
          }
        >
          <Route path="security" element={<SecurityStatisticsPage />} />
          <Route path="security/settings" element={<SecuritySettingsPage />} />
          <Route
            path="security/secrets"
            element={<Navigate to="/security/settings#secrets" replace />}
          />
          <Route
            path="security/verification"
            element={<Navigate to="/security/settings#verification" replace />}
          />
          <Route
            path="security/sessions"
            element={<Navigate to="/security/settings#sessions" replace />}
          />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Stremio"
              tabs={[
                { path: '/addon', label: 'Statistics', icon: ChartBarIcon },
                { path: '/addon/settings', label: 'Settings', icon: GearSixIcon },
                { path: '/addon/history', label: 'History', icon: ClockCounterClockwiseIcon },
              ]}
            />
          }
        >
          <Route path="addon" element={<StremioStatisticsPage />} />
          <Route
            path="addon/settings"
            element={<AIOSourcePage key="stremio" section="stremio" />}
          />
          <Route path="addon/history" element={<StremioHistoryPage />} />
          <Route path="addon/history/:id" element={<StremioRequestPage />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Remote catalogs"
              tabs={[
                { path: '/remotes', label: 'Statistics', icon: ChartBarIcon },
                { path: '/remotes/settings', label: 'Settings', icon: GearSixIcon },
                { path: '/remotes/history', label: 'History', icon: ClockCounterClockwiseIcon },
              ]}
            />
          }
        >
          <Route path="remotes" element={<RemoteStatisticsPage />} />
          <Route path="remotes/settings" element={<RemotesPage />} />
          <Route path="remotes/history" element={<RunsPage remote />} />
          <Route path="remotes/history/:id" element={<RunDetailPage remote />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Shared catalogs"
              tabs={[
                { path: '/sharing', label: 'Statistics', icon: ChartBarIcon },
                { path: '/sharing/settings', label: 'Settings', icon: GearSixIcon },
              ]}
            />
          }
        >
          <Route path="sharing" element={<SharedCatalogStatisticsPage />} />
          <Route path="sharing/settings" element={<SharingPage />} />
        </Route>
        <Route path="torznab" element={<TorznabPage />} />
        <Route
          element={
            <WorkspaceLayout
              title="Public access"
              tabs={[
                { path: '/public-access', label: 'Settings', icon: GearSixIcon },
                { path: '/public-access/limits', label: 'Request limits', icon: GaugeIcon },
              ]}
            />
          }
        >
          <Route path="public-access" element={<PublicAccessPage />} />
          <Route path="public-access/limits" element={<PublicRequestLimitsPage />} />
        </Route>
        <Route path="sharing/remotes" element={<LegacyRemotes />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="network/incoming" element={<IncomingNetworkPage />} />
        <Route
          element={
            <WorkspaceLayout
              title="Outgoing"
              tabs={[
                { path: '/network/outgoing', label: 'Proxies', icon: GlobeIcon },
                { path: '/network/outgoing/routing', label: 'Routing', icon: PathIcon },
              ]}
            />
          }
        >
          <Route path="network/outgoing" element={<NetworkPage />} />
          <Route path="network/outgoing/routing" element={<NetworkRoutingPage />} />
        </Route>
        <Route path="network" element={<Navigate to="/network/incoming" replace />} />
        <Route path="network/proxies" element={<Navigate to="/network/outgoing" replace />} />
        <Route
          path="network/routing"
          element={<Navigate to="/network/outgoing/routing" replace />}
        />
        <Route path="network/limits" element={<Navigate to="/public-access/limits" replace />} />
        <Route path="notifications" element={<LegacyActivity />} />
        <Route path="webhooks" element={<LegacyActivity />} />
        <Route path="torrents" element={<TorrentsPage />} />
        <Route path="search" element={<CatalogSearchPage />} />
        <Route path="search/:id" element={<CatalogTitlePage />} />
        <Route path="secrets" element={<Navigate to="/security/settings#secrets" replace />} />
        <Route path="schedules" element={<Navigate to="/providers/settings" replace />} />
        <Route path="settings" element={<Navigate to="/providers/settings" replace />} />
        <Route path="import-settings" element={<LegacyImportSettings />} />
        <Route
          path="*"
          element={
            <div className="page">
              <Empty
                title="Page not found"
                description="This address does not match a console page."
                action={
                  <Button asChild>
                    <Link to="/">Back to Sources</Link>
                  </Button>
                }
              />
            </div>
          }
        />
      </Route>
    </Route>,
  ),
)

ReactDOM.createRoot(document.getElementById('root')!).render(
  <RootBoundary>
    <React.StrictMode>
      <IconContext.Provider value={{ weight: 'regular', size: 18 }}>
        <QueryClientProvider client={queryClient}>
          <NotificationProvider>
            <RouterProvider router={router} />
          </NotificationProvider>
        </QueryClientProvider>
      </IconContext.Provider>
    </React.StrictMode>
  </RootBoundary>,
)
