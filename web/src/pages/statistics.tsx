import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useLocation } from 'react-router-dom'
import { api, params, LIVE_REFRESH_INTERVAL, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { useListViewState } from '@/lib/detail-navigation'
import { sourceSettingsPath } from '@/lib/source-navigation'
import type { Items, ProviderSummary } from '@/lib/types'
import type { CollectionOverview } from '@/lib/collection-types'
import type { Controls, Status } from '@/lib/aiosource-types'
import { identificationResults, identificationStatuses } from '@/lib/identification'
import { Empty, ErrorNotice, Loading, PageHeader, ResultCount } from '@/components/common'
import { ListFilters, useListFilters } from '@/components/list-filters'
import { compareListNames, matchesListQuery } from '@/lib/list-view'
import { SourceTorrentStats, useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Badge } from '@/components/ui/badge'
import { Statistic } from '@/components/statistic'
import { DebridStatistics } from '@/components/debrid-statistics'
import { number, percentage, useDate } from '@/lib/format'
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

export function AIOStatisticsPage({ section }: { section: 'metadata' | 'debrid' }) {
  return section === 'metadata' ? <MetadataStatisticsPage /> : <DebridStatisticsPage />
}

function MetadataStatisticsPage() {
  const date = useDate()
  const location = useLocation()
  const legacyMatches =
    new URLSearchParams(location.search).has('match_status') ||
    location.hash === '#matching-results'
  const query = useQuery({
    queryKey: ['aiosource'],
    queryFn: ({ signal }) => api<Status>('/aiosource', { signal }),
    refetchInterval: COLLECTION_REFRESH_INTERVAL,
  })
  const status = query.data
  const showBookStatistics = Boolean(
    status?.books &&
    (status.config.openlibrary_enabled ||
      status.books.complete ||
      status.books.position > 0 ||
      status.books.running ||
      status.books.error),
  )
  const identifiedTotal = status
    ? status.status.matched +
      status.status.ambiguous +
      status.status.unmatched +
      status.status.matching_pending
    : 0
  if (legacyMatches)
    return (
      <Navigate to={`/metadata/identification${location.search}`} replace state={location.state} />
    )
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        status && (
          <>
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Statistic label="Information catalog entries" value={status.status.works} />
              <Statistic
                label="Imported torrents"
                value={identifiedTotal}
                to="/metadata/identification"
              />
              {identificationStatuses.map((decision) => (
                <Statistic
                  key={decision}
                  label={
                    decision === 'matched'
                      ? 'Identified torrents'
                      : identificationResults[decision].label
                  }
                  value={`${number(status.status[decision === 'pending' ? 'matching_pending' : decision])} (${percentage(status.status[decision === 'pending' ? 'matching_pending' : decision], identifiedTotal)})`}
                  to={`/metadata/identification?${params({ match_status: decision })}`}
                />
              ))}
              <Statistic label="Film & series information pending" value={status.status.pending} />
              {status.books && showBookStatistics && (
                <>
                  <Statistic label="Book works" value={status.books.works} />
                  <Statistic label="Book editions" value={status.books.editions} />
                  <Statistic label="Book authors" value={status.books.authors} />
                  <Statistic label="Book information pending" value={status.books.pending} />
                </>
              )}
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {status.books && showBookStatistics && (
                <>
                  <Statistic
                    label="Last successful book update"
                    value={
                      status.books.last_sync ? date(status.books.last_sync) : 'No completed update'
                    }
                    size="sm"
                    to="/metadata/history?service=openlibrary"
                  />
                  <Statistic
                    label="Book synchronization"
                    value={bookSyncLabel(status.books, status.config.openlibrary_enabled)}
                    size="sm"
                    to="/metadata/settings"
                  />
                  <Statistic
                    label="Book changes applied through"
                    value={
                      status.books.changes_through
                        ? date(status.books.changes_through)
                        : 'Not established'
                    }
                    size="sm"
                    to="/metadata/history?service=openlibrary"
                  />
                  <Statistic
                    label="Book export date"
                    value={
                      status.books.snapshot_at
                        ? status.books.snapshot_at.slice(0, 10)
                        : 'Not established'
                    }
                    size="sm"
                  />
                  <Statistic
                    label="Next automatic book update"
                    value={
                      status.books.running
                        ? 'Running'
                        : !status.config.openlibrary_enabled
                          ? 'Disabled'
                          : status.books.next_update
                            ? date(status.books.next_update)
                            : 'Waiting'
                    }
                    size="sm"
                    to="/metadata/settings"
                  />
                </>
              )}
              <Statistic
                label="Last successful film & series update"
                value={
                  status.status.last_sync ? date(status.status.last_sync) : 'No completed update'
                }
                size="sm"
                to="/metadata/history?service=tmdb"
              />
              <Statistic
                label="Film & series synchronization"
                value={metadataSyncLabel(
                  status.status,
                  status.syncing,
                  status.config.auto_sync,
                  status.recovery_required,
                )}
                size="sm"
                to="/metadata/settings"
              />
              <Statistic
                label="Film & series changes applied through"
                value={
                  status.status.applied_through
                    ? date(status.status.applied_through)
                    : 'Not established'
                }
                size="sm"
                to="/metadata/history?service=tmdb"
              />
              <Statistic
                label="Film & series export date"
                value={status.status.inventory_date || 'Not established'}
                size="sm"
              />
              <Statistic
                label="Next automatic film & series update"
                value={
                  status.automation.next_update
                    ? date(status.automation.next_update)
                    : status.automation.updates === 'running'
                      ? 'Running'
                      : status.automation.updates === 'disabled'
                        ? 'Disabled'
                        : 'Waiting'
                }
                size="sm"
                to="/metadata/settings"
              />
              <Statistic
                label="Next automatic identification"
                value={
                  status.automation.next_identification
                    ? date(status.automation.next_identification)
                    : status.automation.identification === 'running'
                      ? 'Running'
                      : status.automation.identification === 'disabled'
                        ? 'Disabled'
                        : status.automation.identification === 'up_to_date'
                          ? 'Up to date'
                          : 'Waiting for information'
                }
                size="sm"
                to="/metadata/settings"
              />
            </dl>
            <div className="flex flex-wrap gap-2" aria-label="Current activity">
              <Link
                to="/metadata/history?service=tmdb"
                aria-label="View film & series update history"
              >
                <Badge
                  variant={
                    status.syncing ? 'info' : status.status.sync_error ? 'warning' : 'secondary'
                  }
                >
                  {status.syncing
                    ? 'Film & series update running'
                    : status.status.sync_error
                      ? 'Film & series update needs attention'
                      : 'Film & series update idle'}
                </Badge>
              </Link>
              {status.books && showBookStatistics && (
                <Link
                  to="/metadata/history?service=openlibrary"
                  aria-label="View book update history"
                >
                  <Badge
                    variant={
                      status.books.running
                        ? 'info'
                        : status.books.error || status.books.needs_export
                          ? 'warning'
                          : 'secondary'
                    }
                  >
                    {status.books.running
                      ? 'Book update running'
                      : status.books.error || status.books.needs_export
                        ? 'Book update needs attention'
                        : 'Book update idle'}
                  </Badge>
                </Link>
              )}
              <Link to="/metadata/history?kind=matching" aria-label="View identification history">
                <Badge variant={status.matching ? 'info' : 'secondary'}>
                  {status.matching ? 'Identification running' : 'Identification idle'}
                </Badge>
              </Link>
            </div>
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
