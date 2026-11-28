import {
  ArrowLeftIcon,
  BellIcon,
  ChartBarIcon,
  ClockCounterClockwiseIcon,
  GearSixIcon,
  GlobeIcon,
  IconContext,
  ListChecksIcon,
  ListMagnifyingGlassIcon,
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
  useParams,
} from 'react-router-dom'
import { UnsavedChangesProvider } from '@/lib/unsaved-changes'

import { queryClient } from '@/lib/api'
import { AppShell, RootBoundary, SessionGate } from '@/app'
import { Empty } from '@/components/common'
import { IdentificationMatchesLayout } from '@/components/identification-matches'
import { WorkspaceLayout } from '@/components/workspace-layout'
import { Button } from '@/components/ui/button'
import { InterfacePreferences } from '@/components/interface-preferences'
import { AboutDialog } from '@/components/about-dialog'
import { NotificationProvider } from '@/components/notifications'
import { settingFromHash } from '@/lib/settings-catalog'
import { rawPagePath, rawRecordPath, routes, runDetailPath, stremioRequestPath } from '@/lib/routes'
import { filesExplorerPath, filesPath, sourceSettingsPath } from '@/lib/source-navigation'
import {
  identificationCorrespondencesPath,
  identificationResultsPath,
  legacyMetadataHistory,
  metadataSections,
} from '@/lib/metadata-navigation'
import {
  ActiveAlertsPage,
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
  NotificationChannelsPage,
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

function PreserveRedirect({ to }: { to: string }) {
  const { search, hash, state } = useLocation()
  return <Navigate to={`${to}${search}${hash}`} state={state} replace />
}

function LegacyParamRedirect({ build }: { build: (id: string) => string }) {
  const { id = '' } = useParams()
  const { search, hash, state } = useLocation()
  return <Navigate to={`${build(id)}${search}${hash}`} state={state} replace />
}

function LegacySecuritySettings() {
  const location = useLocation()
  const setting = settingFromHash(location.hash)
  const target = setting?.route.startsWith(`${routes.administration.security.base}/`)
    ? setting.route
    : location.hash === '#sessions'
      ? routes.administration.security.activeSessions
      : location.hash === '#verification' || location.hash.startsWith('#runtime-security')
        ? routes.administration.security.limits
        : routes.administration.security.secrets
  return (
    <Navigate to={`${target}${location.search}${location.hash}`} state={location.state} replace />
  )
}

function LegacyPublicLimits() {
  const location = useLocation()
  return (
    <Navigate
      to={`${routes.administration.security.limits}${location.search}${location.hash || '#public-request-limits'}`}
      state={location.state}
      replace
    />
  )
}

function resolveSecurityLimitsHashTarget(hash: string) {
  const setting = settingFromHash(hash)
  return setting?.route.startsWith(`${routes.administration.security.base}/`)
    ? setting.route
    : hash === '#secrets'
      ? routes.administration.security.secrets
      : hash === '#sessions'
        ? routes.administration.security.activeSessions
        : ''
}

function LegacySecurityLimits() {
  const location = useLocation()
  const target =
    resolveSecurityLimitsHashTarget(location.hash) || routes.administration.security.limits
  return (
    <Navigate to={`${target}${location.search}${location.hash}`} state={location.state} replace />
  )
}

function SecurityLimitsRoute() {
  const location = useLocation()
  const target = resolveSecurityLimitsHashTarget(location.hash)
  if (target && target !== routes.administration.security.limits)
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
      to={`${routes.sources.indexers.torrents.settings}?${search}#advanced-import-settings`}
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
  return (
    <Navigate
      to={`${routes.administration.logs}${search.size ? '?' + search : ''}`}
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
      to={`${run ? runDetailPath(run) : routes.sources.indexers.torrents.history}${target.size ? '?' + target : ''}`}
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

function LegacyIdentificationRoot() {
  const location = useLocation()
  const results = legacyIdentificationResults(location.search, location.hash)
  return (
    <Navigate
      to={
        results || `${routes.metadata.identification.statistics}${location.search}${location.hash}`
      }
      state={location.state}
      replace
    />
  )
}

function LegacyMetadata({ view }: { view: 'statistics' | 'settings' | 'history' }) {
  const location = useLocation()
  const identification = legacyIdentificationResults(location.search, location.hash)
  const settingRoute = settingFromHash(location.hash)?.route
  const settingsPath = settingRoute?.startsWith('/metadata/')
    ? settingRoute
    : routes.metadata.catalogs.videos.settings
  const to =
    identification ||
    (view === 'history'
      ? legacyMetadataHistory(location.search)
      : view === 'settings'
        ? `${settingsPath}${location.search}${location.hash}`
        : `${routes.metadata.catalogs.videos.statistics}${location.search}${location.hash}`)
  return <Navigate to={to} state={location.state} replace />
}

const router = createBrowserRouter(
  createRoutesFromElements(
    <Route
      element={
        <>
          <div className="fixed top-6 right-6 z-(--layer-header-actions) flex items-center gap-2 md:right-7">
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
        <Route
          index
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />

        {/* Sources -> Indexers -> Torrents */}
        <Route
          element={
            <WorkspaceLayout
              title="Torrents"
              tabs={[
                {
                  path: routes.sources.indexers.torrents.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: routes.sources.indexers.torrents.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
                {
                  path: routes.sources.indexers.torrents.history,
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                },
              ]}
            />
          }
        >
          <Route
            path={routes.sources.indexers.torrents.statistics}
            element={<SourceStatisticsPage />}
          />
          <Route path={routes.sources.indexers.torrents.settings} element={<ProvidersPage />} />
          <Route
            path={routes.sources.indexers.torrents.newSource}
            element={<ProviderEditor key="new" />}
          />
          <Route
            path={`${routes.sources.indexers.torrents.settings}/id/:id`}
            element={<ProviderEditor />}
          />
          <Route
            path={`${routes.sources.indexers.torrents.settings}/:id`}
            element={<ProviderEditor />}
          />
          <Route path={routes.sources.indexers.torrents.history} element={<RunsPage />} />
          <Route
            path={`${routes.sources.indexers.torrents.history}/:id`}
            element={<RunDetailPage />}
          />
          <Route path={`${routes.sources.indexers.torrents.raw}/:id`} element={<RawDetailPage />} />
          <Route path={`${routes.sources.indexers.torrents.pages}/:id`} element={<RawPageView />} />
        </Route>

        {/* Sources -> Remote catalogs */}
        <Route
          element={
            <WorkspaceLayout
              title="Remote catalogs"
              tabs={[
                {
                  path: routes.sources.remoteCatalogs.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: routes.sources.remoteCatalogs.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
                {
                  path: routes.sources.remoteCatalogs.history,
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                },
              ]}
            />
          }
        >
          <Route
            path={routes.sources.remoteCatalogs.statistics}
            element={<RemoteStatisticsPage />}
          />
          <Route path={routes.sources.remoteCatalogs.settings} element={<RemotesPage />} />
          <Route path={routes.sources.remoteCatalogs.history} element={<RunsPage remote />} />
          <Route
            path={`${routes.sources.remoteCatalogs.history}/:id`}
            element={<RunDetailPage remote />}
          />
        </Route>

        {/* Sources -> Files */}
        <Route
          element={
            <WorkspaceLayout
              title="Files"
              tabs={[
                { path: filesPath, label: 'Statistics', icon: ChartBarIcon },
                { path: filesExplorerPath, label: 'Explorer', icon: ListMagnifyingGlassIcon },
              ]}
            />
          }
        >
          <Route path={filesPath} element={<FilesStatisticsPage />} />
          <Route path={filesExplorerPath} element={<TorrentsPage />} />
        </Route>

        {/* Metadata -> Catalogs */}
        {(['videos', 'games', 'software', 'music', 'books'] as const).map((section) => {
          const { label, statistics, settings, history } = metadataSections[section]
          return (
            <Route
              key={section}
              element={
                <WorkspaceLayout
                  title={label}
                  tabs={[
                    { path: statistics, label: 'Statistics', icon: ChartBarIcon },
                    { path: settings, label: 'Settings', icon: GearSixIcon },
                    { path: history, label: 'History', icon: ClockCounterClockwiseIcon },
                  ]}
                />
              }
            >
              <Route path={statistics} element={<AIOStatisticsPage section={section} />} />
              <Route path={settings} element={<AIOSourcePage key={section} section={section} />} />
              <Route
                path={history}
                element={<OperationHistoryPage section="metadata" metadataSection={section} />}
              />
              <Route
                path={`${history}/:id`}
                element={<OperationDetailPage section="metadata" metadataSection={section} />}
              />
            </Route>
          )
        })}

        {/* Metadata -> Identification */}
        <Route
          element={
            <WorkspaceLayout
              title="Identification"
              tabs={[
                {
                  path: routes.metadata.identification.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: identificationResultsPath,
                  matches: [identificationResultsPath, identificationCorrespondencesPath],
                  label: 'Matches',
                  icon: ListChecksIcon,
                },
                {
                  path: routes.metadata.identification.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
                {
                  path: routes.metadata.identification.history,
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                },
              ]}
            />
          }
        >
          <Route
            path={routes.metadata.identification.statistics}
            element={<IdentificationStatisticsRoute />}
          />
          <Route
            path={identificationResultsPath}
            element={
              <IdentificationMatchesLayout view="results">
                <MatchingDiagnostics />
              </IdentificationMatchesLayout>
            }
          />
          <Route
            path={identificationCorrespondencesPath}
            element={
              <IdentificationMatchesLayout view="correspondences">
                <ReleaseCorrespondences />
              </IdentificationMatchesLayout>
            }
          />
          <Route
            path={routes.metadata.identification.settings}
            element={<AIOSourcePage key="identification" section="identification" />}
          />
          <Route
            path={routes.metadata.identification.history}
            element={<OperationHistoryPage section="metadata" metadataSection="identification" />}
          />
          <Route
            path={`${routes.metadata.identification.history}/:id`}
            element={<OperationDetailPage section="metadata" metadataSection="identification" />}
          />
        </Route>

        {/* Debrid */}
        <Route
          element={
            <WorkspaceLayout
              title="Debrid"
              tabs={[
                { path: routes.debrid.statistics, label: 'Statistics', icon: ChartBarIcon },
                { path: routes.debrid.settings, label: 'Settings', icon: GearSixIcon },
                { path: routes.debrid.history, label: 'History', icon: ClockCounterClockwiseIcon },
              ]}
            />
          }
        >
          <Route path={routes.debrid.statistics} element={<AIOStatisticsPage section="debrid" />} />
          <Route
            path={routes.debrid.settings}
            element={<AIOSourcePage key="debrid" section="debrid" />}
          />
          <Route
            path={routes.debrid.history}
            element={<OperationHistoryPage key="debrid-history" section="debrid" />}
          />
          <Route
            path={`${routes.debrid.history}/:id`}
            element={<OperationDetailPage section="debrid" />}
          />
        </Route>

        {/* Browse */}
        <Route path={routes.browse.trending} element={<CatalogTrendingPage />} />
        <Route path={routes.browse.search} element={<CatalogSearchPage />} />
        <Route path={routes.browse.searchTorrents} element={<CatalogTorrentsPage />} />
        <Route path={`${routes.browse.search}/:id`} element={<CatalogTitlePage />} />

        {/* Connections */}
        <Route path={routes.connections.torznab} element={<TorznabPage />} />
        <Route
          element={
            <WorkspaceLayout
              title="Stremio"
              tabs={[
                {
                  path: routes.connections.stremio.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: routes.connections.stremio.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
                {
                  path: routes.connections.stremio.history,
                  label: 'History',
                  icon: ClockCounterClockwiseIcon,
                },
              ]}
            />
          }
        >
          <Route path={routes.connections.stremio.statistics} element={<StremioStatisticsPage />} />
          <Route
            path={routes.connections.stremio.settings}
            element={<AIOSourcePage key="stremio" section="stremio" />}
          />
          <Route path={routes.connections.stremio.history} element={<StremioHistoryPage />} />
          <Route
            path={`${routes.connections.stremio.history}/:id`}
            element={<StremioRequestPage />}
          />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Shared catalogs"
              tabs={[
                {
                  path: routes.connections.sharedCatalogs.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: routes.connections.sharedCatalogs.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
              ]}
            />
          }
        >
          <Route
            path={routes.connections.sharedCatalogs.statistics}
            element={<SharedCatalogStatisticsPage />}
          />
          <Route path={routes.connections.sharedCatalogs.settings} element={<SharingPage />} />
        </Route>
        <Route
          element={
            <WorkspaceLayout
              title="Alerts"
              tabs={[
                {
                  path: routes.connections.alerts.active,
                  label: 'Active alerts',
                  icon: BellIcon,
                },
                {
                  path: routes.connections.alerts.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
              ]}
            />
          }
        >
          <Route path={routes.connections.alerts.active} element={<ActiveAlertsPage />} />
          <Route path={routes.connections.alerts.settings} element={<NotificationChannelsPage />} />
        </Route>

        {/* Administration -> Security */}
        <Route path={routes.administration.security.secrets} element={<SecuritySecretsPage />} />
        <Route path={routes.administration.security.limits} element={<SecurityLimitsRoute />} />
        <Route
          path={routes.administration.security.activeSessions}
          element={<SecuritySessionsPage />}
        />

        {/* Administration -> Network */}
        <Route path={routes.administration.network.incoming} element={<IncomingNetworkPage />} />
        <Route
          element={
            <WorkspaceLayout
              title="Outgoing"
              tabs={[
                {
                  path: routes.administration.network.outgoing.proxies,
                  label: 'Proxies',
                  icon: GlobeIcon,
                },
                {
                  path: routes.administration.network.outgoing.routing,
                  label: 'Routing',
                  icon: PathIcon,
                },
              ]}
            />
          }
        >
          <Route path={routes.administration.network.outgoing.proxies} element={<NetworkPage />} />
          <Route
            path={routes.administration.network.outgoing.routing}
            element={<NetworkRoutingPage />}
          />
        </Route>

        {/* Administration -> Users */}
        <Route path={routes.administration.users.trusted} element={<TrustedUsersPage />} />
        <Route path={routes.administration.users.public} element={<PublicAccessPage />} />

        {/* Administration -> Storage */}
        <Route
          element={
            <WorkspaceLayout
              title="Storage"
              tabs={[
                {
                  path: routes.administration.storage.statistics,
                  label: 'Statistics',
                  icon: ChartBarIcon,
                },
                {
                  path: routes.administration.storage.settings,
                  label: 'Settings',
                  icon: GearSixIcon,
                },
              ]}
            />
          }
        >
          <Route path={routes.administration.storage.statistics} element={<HealthPage />} />
          <Route path={routes.administration.storage.settings} element={<StorageSettingsPage />} />
        </Route>

        {/* Administration -> Logs */}
        <Route path={routes.administration.logs} element={<LogsPage />} />

        {/* Canonical group/section entry redirects */}
        <Route
          path="sources"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />
        <Route
          path="sources/indexers"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />
        <Route
          path="sources/indexers/torrents"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />
        <Route
          path="sources/remote-catalogs"
          element={<PreserveRedirect to={routes.sources.remoteCatalogs.statistics} />}
        />
        <Route
          path="sources/files"
          element={<PreserveRedirect to={routes.sources.files.statistics} />}
        />
        <Route
          path="sources/files/results"
          element={<PreserveRedirect to={routes.sources.files.explorer} />}
        />
        <Route
          path="metadata/catalogs"
          element={<PreserveRedirect to={routes.metadata.catalogs.videos.statistics} />}
        />
        {(['videos', 'games', 'software', 'music', 'books'] as const).map((section) => (
          <Route
            key={`catalog-root-${section}`}
            path={`metadata/catalogs/${section}`}
            element={<PreserveRedirect to={metadataSections[section].statistics} />}
          />
        ))}
        <Route path="metadata/identification" element={<LegacyIdentificationRoot />} />
        <Route
          path="metadata/identification/matches"
          element={<PreserveRedirect to={identificationResultsPath} />}
        />
        <Route path="debrid" element={<PreserveRedirect to={routes.debrid.statistics} />} />
        <Route path="browse" element={<PreserveRedirect to={routes.browse.trending} />} />
        <Route path="connections" element={<PreserveRedirect to={routes.connections.torznab} />} />
        <Route
          path="connections/stremio"
          element={<PreserveRedirect to={routes.connections.stremio.statistics} />}
        />
        <Route
          path="connections/shared-catalogs"
          element={<PreserveRedirect to={routes.connections.sharedCatalogs.statistics} />}
        />
        <Route
          path="connections/alerts"
          element={<PreserveRedirect to={routes.connections.alerts.active} />}
        />
        <Route path="alerts" element={<PreserveRedirect to={routes.connections.alerts.active} />} />
        <Route
          path="administration"
          element={<PreserveRedirect to={routes.administration.security.secrets} />}
        />
        <Route path="administration/security" element={<LegacySecuritySettings />} />
        <Route
          path="administration/security/sessions"
          element={<PreserveRedirect to={routes.administration.security.activeSessions} />}
        />
        <Route
          path="administration/network"
          element={<PreserveRedirect to={routes.administration.network.incoming} />}
        />
        <Route
          path="administration/network/outgoing"
          element={<PreserveRedirect to={routes.administration.network.outgoing.proxies} />}
        />
        <Route
          path="administration/users"
          element={<PreserveRedirect to={routes.administration.users.trusted} />}
        />
        <Route
          path="administration/storage"
          element={<PreserveRedirect to={routes.administration.storage.statistics} />}
        />

        {/* Legacy bookmark redirects (preserving search, hash, and history state) */}
        <Route
          path="indexers"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />
        <Route
          path="providers"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.statistics} />}
        />
        <Route
          path="providers/settings"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.settings} />}
        />
        <Route
          path="providers/new"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.newSource} />}
        />
        <Route
          path="providers/id/:id"
          element={<LegacyParamRedirect build={sourceSettingsPath} />}
        />
        <Route path="providers/:id" element={<LegacyParamRedirect build={sourceSettingsPath} />} />
        <Route
          path="runs"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.history} />}
        />
        <Route
          path="runs/:id"
          element={<LegacyParamRedirect build={(id) => runDetailPath(id)} />}
        />
        <Route path="raw" element={<LegacyRawHistory />} />
        <Route path="raw/:id" element={<LegacyParamRedirect build={rawRecordPath} />} />
        <Route path="pages/:id" element={<LegacyParamRedirect build={rawPagePath} />} />
        <Route
          path="remotes"
          element={<PreserveRedirect to={routes.sources.remoteCatalogs.statistics} />}
        />
        <Route
          path="remotes/settings"
          element={<PreserveRedirect to={routes.sources.remoteCatalogs.settings} />}
        />
        <Route
          path="remotes/history"
          element={<PreserveRedirect to={routes.sources.remoteCatalogs.history} />}
        />
        <Route
          path="remotes/history/:id"
          element={<LegacyParamRedirect build={(id) => runDetailPath(id, true)} />}
        />
        <Route
          path="sharing/remotes"
          element={<PreserveRedirect to={routes.sources.remoteCatalogs.settings} />}
        />
        <Route path="files" element={<PreserveRedirect to={routes.sources.files.statistics} />} />
        <Route
          path="files/results"
          element={<PreserveRedirect to={routes.sources.files.explorer} />}
        />
        <Route path="torrents" element={<PreserveRedirect to={routes.sources.files.explorer} />} />
        <Route path="metadata" element={<LegacyMetadata view="statistics" />} />
        <Route path="metadata/settings" element={<LegacyMetadata view="settings" />} />
        <Route path="metadata/history" element={<LegacyMetadata view="history" />} />
        <Route path="metadata/history/:id" element={<OperationDetailPage section="metadata" />} />
        {(['videos', 'games', 'software', 'music', 'books'] as const).map((section) => (
          <React.Fragment key={`legacy-metadata-${section}`}>
            <Route
              path={`metadata/${section}`}
              element={<PreserveRedirect to={metadataSections[section].statistics} />}
            />
            <Route
              path={`metadata/${section}/settings`}
              element={<PreserveRedirect to={metadataSections[section].settings} />}
            />
            <Route
              path={`metadata/${section}/history`}
              element={<PreserveRedirect to={metadataSections[section].history} />}
            />
            <Route
              path={`metadata/${section}/history/:id`}
              element={
                <LegacyParamRedirect
                  build={(id) => `${metadataSections[section].history}/${encodeURIComponent(id)}`}
                />
              }
            />
          </React.Fragment>
        ))}
        <Route path="trending" element={<PreserveRedirect to={routes.browse.trending} />} />
        <Route path="search" element={<PreserveRedirect to={routes.browse.search} />} />
        <Route
          path="search/torrents"
          element={<PreserveRedirect to={routes.browse.searchTorrents} />}
        />
        <Route
          path="search/:id"
          element={
            <LegacyParamRedirect
              build={(id) => `${routes.browse.search}/${encodeURIComponent(id)}`}
            />
          }
        />
        <Route path="torznab" element={<PreserveRedirect to={routes.connections.torznab} />} />
        <Route
          path="addon"
          element={<PreserveRedirect to={routes.connections.stremio.statistics} />}
        />
        <Route
          path="addon/settings"
          element={<PreserveRedirect to={routes.connections.stremio.settings} />}
        />
        <Route
          path="addon/history"
          element={<PreserveRedirect to={routes.connections.stremio.history} />}
        />
        <Route
          path="addon/history/:id"
          element={<LegacyParamRedirect build={stremioRequestPath} />}
        />
        <Route
          path="sharing"
          element={<PreserveRedirect to={routes.connections.sharedCatalogs.statistics} />}
        />
        <Route
          path="sharing/settings"
          element={<PreserveRedirect to={routes.connections.sharedCatalogs.settings} />}
        />
        <Route
          path="health"
          element={<PreserveRedirect to={routes.administration.storage.statistics} />}
        />
        <Route
          path="health/settings"
          element={<PreserveRedirect to={routes.administration.storage.settings} />}
        />
        <Route path="security" element={<LegacySecuritySettings />} />
        <Route path="secrets" element={<LegacySecuritySettings />} />
        <Route
          path="security/secrets"
          element={<PreserveRedirect to={routes.administration.security.secrets} />}
        />
        <Route path="security/limits" element={<LegacySecurityLimits />} />
        <Route path="security/settings" element={<LegacySecurityLimits />} />
        <Route
          path="security/verification"
          element={<PreserveRedirect to={routes.administration.security.limits} />}
        />
        <Route
          path="security/sessions"
          element={<PreserveRedirect to={routes.administration.security.activeSessions} />}
        />
        <Route
          path="users"
          element={<PreserveRedirect to={routes.administration.users.trusted} />}
        />
        <Route
          path="trusted-users"
          element={<PreserveRedirect to={routes.administration.users.trusted} />}
        />
        <Route
          path="public-access"
          element={<PreserveRedirect to={routes.administration.users.public} />}
        />
        <Route path="public-access/limits" element={<LegacyPublicLimits />} />
        <Route
          path="network"
          element={<PreserveRedirect to={routes.administration.network.incoming} />}
        />
        <Route
          path="network/incoming"
          element={<PreserveRedirect to={routes.administration.network.incoming} />}
        />
        <Route
          path="network/outgoing"
          element={<PreserveRedirect to={routes.administration.network.outgoing.proxies} />}
        />
        <Route
          path="network/outgoing/routing"
          element={<PreserveRedirect to={routes.administration.network.outgoing.routing} />}
        />
        <Route
          path="network/proxies"
          element={<PreserveRedirect to={routes.administration.network.outgoing.proxies} />}
        />
        <Route
          path="network/routing"
          element={<PreserveRedirect to={routes.administration.network.outgoing.routing} />}
        />
        <Route path="network/limits" element={<LegacyPublicLimits />} />
        <Route path="logs" element={<PreserveRedirect to={routes.administration.logs} />} />
        <Route path="notifications" element={<LegacyActivity />} />
        <Route path="webhooks" element={<LegacyActivity />} />
        <Route
          path="schedules"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.settings} />}
        />
        <Route
          path="settings"
          element={<PreserveRedirect to={routes.sources.indexers.torrents.settings} />}
        />
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
                    <Link to={routes.sources.indexers.torrents.statistics}>
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
