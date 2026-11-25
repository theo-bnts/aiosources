import {
  ArrowLeftIcon,
  ChartBarIcon,
  ClockCounterClockwiseIcon,
  GearSixIcon,
  GlobeIcon,
  IconContext,
  LinkIcon,
  ListChecksIcon,
  PathIcon,
} from '@phosphor-icons/react'
import { OccurrenceNavigationProvider } from '@/lib/occurrence-navigation'
import { OccurrenceDialogHost } from '@/components/occurrence-dialog-host'
import React from 'react'
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

import { queryClient } from '@/lib/api'
import { AppShell, RootBoundary, SessionGate } from '@/app'
import { Empty } from '@/components/common'
import { WorkspaceLayout } from '@/components/workspace-layout'
import { Button } from '@/components/ui/button'
import { InterfacePreferences } from '@/components/interface-preferences'
import { AboutDialog } from '@/components/about-dialog'
import { NotificationProvider } from '@/components/notifications'
import { settingFromHash } from '@/lib/settings-catalog'
import { filesPath, filesResultsPath } from '@/lib/source-navigation'
import {
  identificationCorrespondencesPath,
  identificationResultsPath,
  legacyMetadataHistory,
  metadataSections,
} from '@/lib/metadata-navigation'
import {
  AIOSourcePage,
  AIOStatisticsPage,
  CatalogSearchPage,
  CatalogTitlePage,
  CatalogTorrentsPage,
  CatalogTrendingPage,
  FilesStatisticsPage,
  HealthPage,
  IdentificationStatistics,
  IncomingNetworkPage,
  LogsPage,
  MatchingDiagnostics,
  NetworkPage,
  NetworkRoutingPage,
  OperationDetailPage,
  OperationHistoryPage,
  ProviderEditor,
  ProvidersPage,
  PublicAccessPage,
  RawDetailPage,
  RawPageView,
  ReleaseCorrespondences,
  RemotesPage,
  RemoteStatisticsPage,
  RunDetailPage,
  RunsPage,
  SecurityLimitsPage,
  SecuritySecretsPage,
  SecuritySessionsPage,
  SharedCatalogStatisticsPage,
  SharingPage,
  SourceStatisticsPage,
  StorageSettingsPage,
  StremioHistoryPage,
  StremioRequestPage,
  StremioStatisticsPage,
  TorrentsPage,
  TorznabPage,
  TrustedUsersPage,
  prefetchRoute,
} from '@/lib/route-prefetch'
import './index.css'

prefetchRoute(window.location.pathname)

function LegacySecuritySettings() {
  const location = useLocation()
  const setting = settingFromHash(location.hash)
  const target = setting?.route.startsWith('/security/')
    ? setting.route
    : location.hash === '#sessions'
      ? '/security/sessions'
      : location.hash === '#verification' || location.hash.startsWith('#runtime-security')
        ? '/security/limits'
        : '/security/secrets'
  return (
    <Navigate to={`${target}${location.search}${location.hash}`} state={location.state} replace />
  )
}
function LegacySecurityLimits() {
  const location = useLocation()
  return (
    <Navigate
      to={`/security/limits${location.search}${location.hash}`}
      state={location.state}
      replace
    />
  )
}
function LegacyPublicLimits() {
  const location = useLocation()
  return (
    <Navigate
      to={`/security/limits${location.search}${location.hash || '#public-request-limits'}`}
      state={location.state}
      replace
    />
  )
}
function SecurityLimitsRoute() {
  const location = useLocation()
  const setting = settingFromHash(location.hash)
  const target = setting?.route.startsWith('/security/')
    ? setting.route
    : location.hash === '#secrets'
      ? '/security/secrets'
      : location.hash === '#sessions'
        ? '/security/sessions'
        : ''
  if (target && target !== '/security/limits')
    return (
      <Navigate to={`${target}${location.search}${location.hash}`} state={location.state} replace />
    )
  return <SecurityLimitsPage />
}
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
  const rawRun = search.get('run') || search.get('run_id')
  const run = rawRun && /^[A-Za-z0-9_-]{1,128}$/.test(rawRun) ? rawRun : null
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

function legacyIdentificationResults(search: string, hash: string) {
  const params = new URLSearchParams(search)
  const hasFilters = [
    'match_status',
    'match_provider',
    'match_q',
    'match_limit',
    'match_offset',
  ].some((key) => params.has(key))
  return hasFilters || hash === '#matching-results' ? identificationResultsPath + search + hash : ''
}

function IdentificationStatisticsRoute() {
  const location = useLocation()
  const results = legacyIdentificationResults(location.search, location.hash)
  return results ? (
    <Navigate to={results} state={location.state} replace />
  ) : (
    <IdentificationStatistics />
  )
}

function LegacyFiles() {
  const { search, hash, state } = useLocation()
  return <Navigate to={filesResultsPath + search + hash} state={state} replace />
}

function LegacyMetadata({ view }: { view: 'statistics' | 'settings' | 'history' }) {
  const location = useLocation()
  const identification = legacyIdentificationResults(location.search, location.hash)
  const settingRoute = settingFromHash(location.hash)?.route
  const settingsPath = settingRoute?.startsWith('/metadata/')
    ? settingRoute
    : '/metadata/videos/settings'
  const to =
    identification ||
    (view === 'history'
      ? legacyMetadataHistory(location.search)
      : view === 'settings'
        ? `${settingsPath}${location.search}${location.hash}`
        : `/metadata/videos${location.search}${location.hash}`)
  return <Navigate to={to} state={location.state} replace />
}

const router = createBrowserRouter(
  createRoutesFromElements(
    <Route
      element={
        <>
          <div className="fixed top-3 right-5 z-(--layer-header-actions) flex items-center gap-2 md:right-9">
            <InterfacePreferences />
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
      <Route
        element={
          <OccurrenceNavigationProvider>
            <AppShell />
            <OccurrenceDialogHost />
          </OccurrenceNavigationProvider>
        }
      >
        <Route index element={<Navigate to="/providers" replace />} />
        <Route
          element={
            <WorkspaceLayout
              title="Torrents"
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
          <Route path="providers/id/:id" element={<ProviderEditor />} />
          <Route path="providers/:id" element={<ProviderEditor />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="runs/:id" element={<RunDetailPage />} />
          <Route path="raw" element={<LegacyRawHistory />} />
          <Route path="raw/:id" element={<RawDetailPage />} />
          <Route path="pages/:id" element={<RawPageView />} />
        </Route>
        <Route path="metadata/catalogs" element={<Navigate to="/metadata/videos" replace />} />
        <Route path="metadata" element={<LegacyMetadata view="statistics" />} />
        <Route path="metadata/settings" element={<LegacyMetadata view="settings" />} />
        <Route path="metadata/history" element={<LegacyMetadata view="history" />} />
        <Route path="metadata/history/:id" element={<OperationDetailPage section="metadata" />} />
        {(['videos', 'games', 'software', 'music', 'books'] as const).map((section) => {
          const { path, label } = metadataSections[section]
          return (
            <Route
              key={section}
              element={
                <WorkspaceLayout
                  title={label}
                  tabs={[
                    { path, label: 'Statistics', icon: ChartBarIcon },
                    { path: `${path}/settings`, label: 'Settings', icon: GearSixIcon },
                    { path: `${path}/history`, label: 'History', icon: ClockCounterClockwiseIcon },
                  ]}
                />
              }
            >
              <Route path={path} element={<AIOStatisticsPage section={section} />} />
              <Route
                path={`${path}/settings`}
                element={<AIOSourcePage key={section} section={section} />}
              />
              <Route
                path={`${path}/history`}
                element={<OperationHistoryPage section="metadata" metadataSection={section} />}
              />
              <Route
                path={`${path}/history/:id`}
                element={<OperationDetailPage section="metadata" metadataSection={section} />}
              />
            </Route>
          )
        })}
        <Route
          element={
            <WorkspaceLayout
              title="Identification"
              tabs={[
                {
                  path: metadataSections.identification.path,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                { path: identificationResultsPath, label: 'Results', icon: ListChecksIcon },
                {
                  path: identificationCorrespondencesPath,
                  label: 'Correspondences',
                  icon: LinkIcon,
                },
                { path: '/metadata/identification/settings', label: 'Settings', icon: GearSixIcon },
                {
                  path: '/metadata/identification/history',
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                },
              ]}
            />
          }
        >
          <Route
            path={metadataSections.identification.path}
            element={<IdentificationStatisticsRoute />}
          />
          <Route path={identificationResultsPath} element={<MatchingDiagnostics />} />
          <Route path={identificationCorrespondencesPath} element={<ReleaseCorrespondences />} />
          <Route
            path="metadata/identification/settings"
            element={<AIOSourcePage key="identification" section="identification" />}
          />
          <Route
            path="metadata/identification/history"
            element={<OperationHistoryPage section="metadata" metadataSection="identification" />}
          />
          <Route
            path="metadata/identification/history/:id"
            element={<OperationDetailPage section="metadata" metadataSection="identification" />}
          />
        </Route>
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
        <Route path="security" element={<LegacySecuritySettings />} />
        <Route path="security/limits" element={<SecurityLimitsRoute />} />
        <Route path="security/settings" element={<LegacySecurityLimits />} />
        <Route path="security/secrets" element={<SecuritySecretsPage />} />
        <Route path="security/verification" element={<Navigate to="/security/limits" replace />} />
        <Route path="security/sessions" element={<SecuritySessionsPage />} />
        <Route path="users" element={<Navigate to="/trusted-users" replace />} />
        <Route path="trusted-users" element={<TrustedUsersPage />} />
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
        <Route path="public-access" element={<PublicAccessPage />} />
        <Route path="public-access/limits" element={<LegacyPublicLimits />} />
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
        <Route path="network/limits" element={<LegacyPublicLimits />} />
        <Route path="notifications" element={<LegacyActivity />} />
        <Route path="webhooks" element={<LegacyActivity />} />
        <Route path="indexers" element={<Navigate to="/providers" replace />} />
        <Route
          element={
            <WorkspaceLayout
              title="Files"
              tabs={[
                { path: filesPath, label: 'Statistics', icon: ChartBarIcon },
                { path: filesResultsPath, label: 'Results', icon: ListChecksIcon },
              ]}
            />
          }
        >
          <Route path={filesPath} element={<FilesStatisticsPage />} />
          <Route path={filesResultsPath} element={<TorrentsPage />} />
        </Route>
        <Route path="torrents" element={<LegacyFiles />} />
        <Route path="trending" element={<CatalogTrendingPage />} />
        <Route path="search" element={<CatalogSearchPage />} />
        <Route path="search/torrents" element={<CatalogTorrentsPage />} />
        <Route path="search/:id" element={<CatalogTitlePage />} />
        <Route path="secrets" element={<LegacySecuritySettings />} />
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
                    <Link to="/">
                      <ArrowLeftIcon aria-hidden="true" />
                      Back to Sources
                    </Link>
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
