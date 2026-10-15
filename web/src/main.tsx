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
  KeyIcon,
  ShieldCheckIcon,
  DevicesIcon,
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
const SecretsPage = lazy(() => import('@/pages/secrets'))
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
const SharingPage = lazy(() => import('@/pages/catalog-sharing'))
const HealthPage = lazy(() => import('@/pages/health').then((m) => ({ default: m.HealthPage })))
const SecurityPage = lazy(() =>
  import('@/pages/security').then((m) => ({ default: m.SecurityPage })),
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
      to={`/sharing/remotes${location.search}${location.hash}`}
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
          <SessionGate>
            <UnsavedChangesProvider>
              <Outlet />
            </UnsavedChangesProvider>
          </SessionGate>
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
        <Route path="health" element={<HealthPage />} />
        <Route
          element={
            <WorkspaceLayout
              title="Security"
              tabs={[
                { path: '/security', label: 'Secrets', icon: KeyIcon },
                {
                  path: '/security/verification',
                  label: 'Two-step verification',
                  icon: ShieldCheckIcon,
                },
                { path: '/security/sessions', label: 'Active sessions', icon: DevicesIcon },
              ]}
            />
          }
        >
          <Route path="security" element={<SecretsPage />} />
          <Route path="security/secrets" element={<Navigate to="/security" replace />} />
          <Route path="security/verification" element={<SecurityPage view="mfa" />} />
          <Route path="security/sessions" element={<SecurityPage view="sessions" />} />
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
        <Route path="remotes" element={<LegacyRemotes />} />
        <Route path="sharing" element={<SharingPage />} />
        <Route path="sharing/remotes" element={<SharingPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="notifications" element={<LegacyActivity />} />
        <Route path="webhooks" element={<LegacyActivity />} />
        <Route path="torrents" element={<TorrentsPage />} />
        <Route path="secrets" element={<Navigate to="/security" replace />} />
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
