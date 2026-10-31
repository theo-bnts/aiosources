import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useLocation } from 'react-router-dom'
import { api, LIVE_REFRESH_INTERVAL, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { useListViewState } from '@/lib/detail-navigation'
import { sourceSettingsPath } from '@/lib/source-navigation'
import type { Items, ProviderSummary } from '@/lib/types'
import type { CollectionOverview } from '@/lib/collection-types'
import type { Controls, InformationStatus } from '@/lib/aiosource-types'
import { Empty, ErrorNotice, Loading, PageHeader, ResultCount } from '@/components/common'
import { ListFilters, useListFilters } from '@/components/list-filters'
import { compareListNames, matchesListQuery } from '@/lib/list-view'
import { SourceTorrentStats, useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Badge } from '@/components/ui/badge'
import { Disclosure } from '@/components/ui/disclosure'
import { Statistic } from '@/components/statistic'
import { DebridStatistics } from '@/components/debrid-statistics'
import { useDate } from '@/lib/format'
import { bookSyncLabel, metadataSyncLabel } from '@/lib/information-sync'

export function SourceStatisticsPage() {
  const location = useLocation()
  const listState = useListViewState()
  const filters = useListFilters()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const overview = useQuery({
    queryKey: ['collection-settings'],
    queryFn: ({ signal }) => api<CollectionOverview>('/settings/collections', { signal }),
    refetchInterval: COLLECTION_REFRESH_INTERVAL,
  })
  const items = (providers.data?.items ?? [])
    .filter((source) => !source.remote_catalog)
    .sort(compareListNames)
  const counts = useSourceTorrentStats(Boolean(providers.data))
  const visible = items.filter(
    (source) =>
      matchesListQuery(filters.query, source.name, source.id, source.adapter) &&
      (!filters.state ||
        (filters.state === 'enabled' && source.enabled) ||
        (filters.state === 'disabled' && !source.enabled) ||
        (filters.state === 'invalid' && !source.valid)),
  )
  const filtered = Boolean(filters.query || filters.state)
  // Keep existing bookmarks to advanced settings useful after separating views.
  if (new URLSearchParams(location.search).get('settings') === 'advanced')
    return (
      <Navigate
        to={`/providers/settings${location.search}${location.hash}`}
        state={location.state}
        replace
      />
    )
  return (
    <div className="page">
      <PageHeader title="Statistics" />
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      <ErrorNotice error={overview.error} retry={() => void overview.refetch()} />
      <ErrorNotice error={counts.error} retry={() => void counts.refetch()} />
      {overview.data && (
        <dl className="grid grid-cols-2 gap-3 xl:grid-cols-5">
          <Statistic
            label="Imported torrents"
            value={counts.totals ? counts.totals.imported : 'Not available'}
            loadingLabel={counts.isPending && !providers.isError ? 'Loading counts…' : undefined}
            to="/torrents"
          />
          <Statistic
            label="Awaiting import completion"
            value={counts.totals ? counts.totals.awaiting_import : 'Not available'}
            loadingLabel={counts.isPending && !providers.isError ? 'Loading counts…' : undefined}
            to="/runs"
          />
          <Statistic
            label="Running imports"
            value={overview.data.source_running}
            to="/runs?status=running"
          />
          <Statistic
            label="Queued imports"
            value={overview.data.source_queued}
            to="/runs?status=queued"
          />
          <Statistic
            label="Import capacity on this instance"
            value={overview.data.capacity}
            className="col-span-2 xl:col-span-1"
          />
        </dl>
      )}
      {counts.data && (
        <p className="help">
          Published and awaiting records are counted separately. Storage estimates include source
          history and indexes; shared metadata and caches are separate.
        </p>
      )}
      {providers.data && (items.length > 0 || filtered) && (
        <ListFilters
          filters={filters}
          states={[
            { value: 'enabled', label: 'Enabled' },
            { value: 'disabled', label: 'Disabled' },
            { value: 'invalid', label: 'Needs fixing' },
          ]}
          count={visible.length}
          total={items.length}
        />
      )}
      {providers.data && !items.length && !filtered && <ResultCount total={0} label="sources" />}
      {providers.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="grid items-start gap-5 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((source) => (
            <section key={source.id} className="section-card">
              <h2 className="section-title">
                <Link className="data-link" to={sourceSettingsPath(source.id)} state={listState}>
                  {source.name || source.id}
                </Link>
              </h2>
              <SourceTorrentStats
                source={source.name || source.id}
                counts={counts.bySource.get(source.id)}
                unavailable={counts.isError}
                totals={counts.totals}
              />
            </section>
          ))}
        </div>
      ) : (
        !providers.isError && (
          <Empty
            title={filtered ? 'No sources match these filters' : 'No sources configured'}
            description={
              filtered
                ? 'Clear the filters to see your other sources.'
                : 'Add a source to start importing.'
            }
          />
        )
      )}
    </div>
  )
}

export function AIOStatisticsPage({ section }: { section: 'videos' | 'books' | 'debrid' }) {
  return section === 'debrid' ? (
    <DebridStatisticsPage />
  ) : (
    <MetadataStatisticsPage section={section} />
  )
}

function MetadataStatisticsPage({ section }: { section: 'videos' | 'books' }) {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource', 'information'],
    queryFn: ({ signal }) => api<InformationStatus>('/aiosource/information', { signal }),
    // A reset can finish between ordinary statistics refreshes. Follow it at
    // the control cadence, then return to the normal statistics interval.
    refetchInterval: (query) =>
      query.state.data?.maintenance?.status === 'running'
        ? LIVE_REFRESH_INTERVAL()
        : COLLECTION_REFRESH_INTERVAL(),
  })
  const status = query.data
  const books = status?.books
  const book = section === 'books'
  const path = `/metadata/${section}`
  const running = book ? Boolean(books?.running) : Boolean(status?.syncing)
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === section || status.maintenance.scope === 'metadata')
  const automatic = book ? status?.config.openlibrary_enabled : status?.config.auto_sync
  const lastSync = book ? books?.last_sync : status?.status.last_sync
  const appliedThrough = book ? books?.changes_through : status?.status.applied_through
  const snapshot = book ? books?.snapshot_at?.slice(0, 10) : status?.status.inventory_date
  const next = book ? books?.next_update : status?.automation.next_update
  const needsAttention = book
    ? Boolean(books?.error || books?.needs_export)
    : Boolean(
        status?.status.sync_error ||
        status?.recovery_required ||
        status?.status.recovery_started_at,
      )
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        status && (
          <>
            <dl className={`grid grid-cols-2 gap-3 ${book ? 'lg:grid-cols-4' : ''}`}>
              <Statistic
                label="Catalog entries"
                value={book ? (books?.works ?? 0) : status.status.video_works}
              />
              {book && (
                <>
                  <Statistic label="Editions" value={books?.editions ?? 0} />
                  <Statistic label="Authors" value={books?.authors ?? 0} />
                </>
              )}
              <Statistic
                label="Information pending"
                value={book ? (books?.pending ?? 0) : status.status.pending}
                to={`${path}/history`}
              />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Synchronization"
                value={
                  resetting
                    ? 'Resetting information'
                    : book
                      ? books
                        ? bookSyncLabel(books, automatic)
                        : 'Not started'
                      : metadataSyncLabel(
                          status.status,
                          running,
                          automatic,
                          status.recovery_required,
                        )
                }
                size="sm"
                to={`${path}/settings`}
              />
              <Statistic
                label="Last successful update"
                value={lastSync ? date(lastSync) : 'No completed update'}
                size="sm"
                to={`${path}/history`}
              />
              <Statistic
                label="Changes applied through"
                value={appliedThrough ? date(appliedThrough) : 'Not established'}
                size="sm"
                to={`${path}/history`}
              />
              <Statistic label="Export date" value={snapshot || 'Not established'} size="sm" />
              <Statistic
                label="Next automatic update"
                value={
                  resetting
                    ? 'Waiting for reset'
                    : running
                      ? 'Running'
                      : !automatic
                        ? 'Disabled'
                        : next
                          ? date(next)
                          : 'Waiting'
                }
                size="sm"
                to={`${path}/settings`}
              />
            </dl>
            <div className="flex flex-wrap gap-2" aria-label="Current activity">
              <Link to={`${path}/history`} aria-label="View update history">
                <Badge
                  variant={running || resetting ? 'info' : needsAttention ? 'warning' : 'secondary'}
                >
                  {resetting
                    ? 'Reset in progress'
                    : running
                      ? 'Update running'
                      : needsAttention
                        ? 'Update needs attention'
                        : 'Update idle'}
                </Badge>
              </Link>
            </div>
            {book && books && books.position > 0 && (
              <Disclosure title="Import details">
                <dl className="grid gap-4 sm:grid-cols-3">
                  <div>
                    <dt className="text-sm text-muted-foreground">Records read from exports</dt>
                    <dd className="mt-1 font-medium tabular-nums">
                      {books.position.toLocaleString()}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-sm text-muted-foreground">Excluded by catalog filters</dt>
                    <dd className="mt-1 font-medium tabular-nums">
                      {(books.filtered ?? 0).toLocaleString()}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-sm text-muted-foreground">Unreadable records</dt>
                    <dd className="mt-1 font-medium tabular-nums">
                      {books.skipped.toLocaleString()}
                    </dd>
                  </div>
                </dl>
              </Disclosure>
            )}
          </>
        )
      )}
    </div>
  )
}

function DebridStatisticsPage() {
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const status = query.data
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        status && (
          <>
            <DebridStatistics config={status.config} />
            <dl className="grid gap-3 sm:grid-cols-2">
              <Statistic
                label="Temporary AllDebrid magnets pending cleanup"
                value={status.status.cleanup_pending}
              />
              <Statistic
                label="Temporary AllDebrid magnets needing attention"
                value={status.status.cleanup_failed}
              />
            </dl>
            {status.status.cleanup_failed > 0 && (
              <p role="alert" className="notice notice-warning">
                Retry temporary magnet cleanup in{' '}
                <Link className="underline" to="/debrid/settings#maintenance">
                  Settings
                </Link>
                .
              </p>
            )}
          </>
        )
      )}
    </div>
  )
}
