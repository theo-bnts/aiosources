import { createElement, lazy, type ComponentProps, type ComponentType } from 'react'

import {
  PRIMARY_ADMIN_ROUTE_CHUNKS,
  resolveRouteChunkKey,
  type RouteChunkKey,
} from '@/lib/route-prefetch-paths'

type ModuleLoader<M extends Record<string, unknown>> = {
  get: () => M | null
  load: () => Promise<M>
  preload: () => void
}

function createModuleLoader<M extends Record<string, unknown>>(
  load: () => Promise<M>,
): ModuleLoader<M> {
  let resolved: M | null = null
  let pending: Promise<M> | null = null
  const loadOnce = () => {
    if (resolved) return Promise.resolve(resolved)
    if (!pending) {
      pending = load().then(
        (mod) => {
          resolved = mod
          return mod
        },
        (error) => {
          pending = null
          throw error
        },
      )
    }
    return pending
  }
  return {
    get: () => resolved,
    load: loadOnce,
    preload: () => {
      void loadOnce().catch(() => undefined)
    },
  }
}

function preloadableComponent<
  M extends Record<string, unknown>,
  K extends keyof M,
  C extends ComponentType<any> = M[K] extends ComponentType<any> ? M[K] : ComponentType<any>,
>(loader: ModuleLoader<M>, exportName: K): ComponentType<ComponentProps<C>> {
  const Lazy = lazy(() => loader.load().then((mod) => ({ default: mod[exportName] as C })))
  function Preloadable(props: ComponentProps<C>) {
    const mod = loader.get()
    if (mod) return createElement(mod[exportName] as C, props)
    return createElement(Lazy, props)
  }
  Preloadable.displayName = `Preloadable(${String(exportName)})`
  return Preloadable
}

const statisticsModule = createModuleLoader(() => import('@/pages/statistics'))
const providersModule = createModuleLoader(() => import('@/pages/providers'))
const runsModule = createModuleLoader(() => import('@/pages/runs'))
const filesStatisticsModule = createModuleLoader(() => import('@/pages/files-statistics'))
const libraryModule = createModuleLoader(() => import('@/pages/library'))
const matchingDiagnosticsModule = createModuleLoader(
  () => import('@/components/matching-diagnostics'),
)
const releaseCorrespondencesModule = createModuleLoader(
  () => import('@/components/release-correspondences'),
)
const identificationStatisticsModule = createModuleLoader(
  () => import('@/components/identification-statistics'),
)
const stremioHistoryModule = createModuleLoader(() => import('@/pages/stremio-history'))
const logsModule = createModuleLoader(() => import('@/pages/logs'))
const networkModule = createModuleLoader(() => import('@/pages/network'))
const incomingNetworkModule = createModuleLoader(() => import('@/pages/incoming-network'))
const sharingModule = createModuleLoader(() => import('@/pages/sharing'))
const remotesModule = createModuleLoader(() => import('@/pages/remotes'))
const publicAccessModule = createModuleLoader(() => import('@/pages/public-access'))
const trustedUsersModule = createModuleLoader(() => import('@/pages/trusted-users'))
const torznabModule = createModuleLoader(() => import('@/pages/torznab'))
const healthModule = createModuleLoader(() => import('@/pages/health'))
const securityModule = createModuleLoader(() => import('@/pages/security'))
const aiosourceModule = createModuleLoader(() => import('@/pages/aiosource'))
const catalogSearchModule = createModuleLoader(() => import('@/pages/catalog-search'))
const catalogTrendingModule = createModuleLoader(() => import('@/pages/catalog-trending'))
const catalogTorrentsModule = createModuleLoader(() => import('@/pages/catalog-torrents'))
const operationHistoryModule = createModuleLoader(() => import('@/pages/operation-history'))

const routeLoaders: Record<RouteChunkKey, () => void> = {
  statistics: statisticsModule.preload,
  providers: providersModule.preload,
  runs: runsModule.preload,
  'files-statistics': filesStatisticsModule.preload,
  library: libraryModule.preload,
  'matching-diagnostics': matchingDiagnosticsModule.preload,
  'release-correspondences': releaseCorrespondencesModule.preload,
  'identification-statistics': identificationStatisticsModule.preload,
  'stremio-history': stremioHistoryModule.preload,
  logs: logsModule.preload,
  network: networkModule.preload,
  'incoming-network': incomingNetworkModule.preload,
  sharing: sharingModule.preload,
  remotes: remotesModule.preload,
  'public-access': publicAccessModule.preload,
  'trusted-users': trustedUsersModule.preload,
  torznab: torznabModule.preload,
  health: healthModule.preload,
  security: securityModule.preload,
  aiosource: aiosourceModule.preload,
  'catalog-search': catalogSearchModule.preload,
  'catalog-trending': catalogTrendingModule.preload,
  'catalog-torrents': catalogTorrentsModule.preload,
  'operation-history': operationHistoryModule.preload,
}

export const SourceStatisticsPage = preloadableComponent(statisticsModule, 'SourceStatisticsPage')
export const AIOStatisticsPage = preloadableComponent(statisticsModule, 'AIOStatisticsPage')
export const ProvidersPage = preloadableComponent(providersModule, 'ProvidersPage')
export const ProviderEditor = preloadableComponent(providersModule, 'ProviderEditor')
export const RunsPage = preloadableComponent(runsModule, 'RunsPage')
export const RunDetailPage = preloadableComponent(runsModule, 'RunDetailPage')
export const FilesStatisticsPage = preloadableComponent(filesStatisticsModule, 'default')
export const TorrentsPage = preloadableComponent(libraryModule, 'TorrentsPage')
export const RawDetailPage = preloadableComponent(libraryModule, 'RawDetailPage')
export const RawPageView = preloadableComponent(libraryModule, 'RawPageView')
export const MatchingDiagnostics = preloadableComponent(
  matchingDiagnosticsModule,
  'MatchingDiagnostics',
)
export const ReleaseCorrespondences = preloadableComponent(
  releaseCorrespondencesModule,
  'ReleaseCorrespondences',
)
export const IdentificationStatistics = preloadableComponent(
  identificationStatisticsModule,
  'IdentificationStatistics',
)
export const StremioStatisticsPage = preloadableComponent(
  stremioHistoryModule,
  'StremioStatisticsPage',
)
export const StremioHistoryPage = preloadableComponent(stremioHistoryModule, 'StremioHistoryPage')
export const StremioRequestPage = preloadableComponent(stremioHistoryModule, 'StremioRequestPage')
export const LogsPage = preloadableComponent(logsModule, 'LogsPage')
export const NetworkPage = preloadableComponent(networkModule, 'NetworkPage')
export const NetworkRoutingPage = preloadableComponent(networkModule, 'NetworkRoutingPage')
export const IncomingNetworkPage = preloadableComponent(incomingNetworkModule, 'default')
export const SharingPage = preloadableComponent(sharingModule, 'default')
export const SharedCatalogStatisticsPage = preloadableComponent(
  sharingModule,
  'SharedCatalogStatisticsPage',
)
export const RemotesPage = preloadableComponent(remotesModule, 'default')
export const RemoteStatisticsPage = preloadableComponent(remotesModule, 'RemoteStatisticsPage')
export const PublicAccessPage = preloadableComponent(publicAccessModule, 'default')
export const TrustedUsersPage = preloadableComponent(trustedUsersModule, 'default')
export const TorznabPage = preloadableComponent(torznabModule, 'default')
export const HealthPage = preloadableComponent(healthModule, 'HealthPage')
export const StorageSettingsPage = preloadableComponent(healthModule, 'StorageSettingsPage')
export const SecuritySecretsPage = preloadableComponent(securityModule, 'SecuritySecretsPage')
export const SecurityLimitsPage = preloadableComponent(securityModule, 'SecurityLimitsPage')
export const SecuritySessionsPage = preloadableComponent(securityModule, 'SecuritySessionsPage')
export const AIOSourcePage = preloadableComponent(aiosourceModule, 'default')
export const CatalogSearchPage = preloadableComponent(catalogSearchModule, 'CatalogSearchPage')
export const CatalogTitlePage = preloadableComponent(catalogSearchModule, 'CatalogTitlePage')
export const CatalogTrendingPage = preloadableComponent(
  catalogTrendingModule,
  'CatalogTrendingPage',
)
export const CatalogTorrentsPage = preloadableComponent(
  catalogTorrentsModule,
  'CatalogTorrentsPage',
)
export const OperationHistoryPage = preloadableComponent(
  operationHistoryModule,
  'OperationHistoryPage',
)
export const OperationDetailPage = preloadableComponent(
  operationHistoryModule,
  'OperationDetailPage',
)

export function prefetchChunk(key: RouteChunkKey) {
  routeLoaders[key]()
}

export function prefetchRoute(path: string) {
  const key = resolveRouteChunkKey(path)
  if (key) prefetchChunk(key)
}

type ConnectionHint = {
  saveData?: boolean
  effectiveType?: string
}

function shouldSkipBackgroundPrefetch(): boolean {
  if (typeof navigator === 'undefined') return true
  const connection = (navigator as Navigator & { connection?: ConnectionHint }).connection
  if (connection?.saveData) return true
  return connection?.effectiveType === '2g' || connection?.effectiveType === 'slow-2g'
}

export function schedulePrimaryRoutePrefetch(): () => void {
  if (typeof window === 'undefined' || shouldSkipBackgroundPrefetch()) {
    return () => undefined
  }
  let cancelled = false
  let index = 0
  let handle: number | undefined
  const useIdle = typeof window.requestIdleCallback === 'function'

  const runBatch = () => {
    handle = undefined
    if (cancelled) return
    const end = Math.min(index + 4, PRIMARY_ADMIN_ROUTE_CHUNKS.length)
    for (; index < end; index++) {
      prefetchChunk(PRIMARY_ADMIN_ROUTE_CHUNKS[index])
    }
    if (index < PRIMARY_ADMIN_ROUTE_CHUNKS.length) {
      scheduleNext(200)
    }
  }

  const scheduleNext = (delayMs: number) => {
    if (useIdle) {
      handle = window.requestIdleCallback(runBatch, { timeout: delayMs + 800 })
    } else {
      handle = window.setTimeout(runBatch, delayMs)
    }
  }

  scheduleNext(150)

  return () => {
    cancelled = true
    if (handle === undefined) return
    if (useIdle && typeof window.cancelIdleCallback === 'function') {
      window.cancelIdleCallback(handle)
    } else {
      window.clearTimeout(handle)
    }
  }
}
