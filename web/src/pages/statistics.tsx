import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useLocation } from 'react-router-dom'
import { api, params } from '@/lib/api'
import { useListViewState } from '@/lib/detail-navigation'
import type { Items, ProviderSummary } from '@/lib/types'
import type { CollectionOverview } from '@/lib/collection-types'
import type { Status } from '@/lib/aiosource-types'
import { Empty, ErrorNotice, Loading, PageHeader } from '@/components/common'
import { SourceFilters, sourceMatches, useSourceFilters } from '@/components/source-filters'
import { SourceTorrentStats, useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Badge } from '@/components/ui/badge'
import { Statistic } from '@/components/statistic'

export function SourceStatisticsPage() {
  const location = useLocation()
  const listState = useListViewState()
  const filters = useSourceFilters()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const overview = useQuery({
    queryKey: ['collection-settings'],
    queryFn: ({ signal }) => api<CollectionOverview>('/settings/collections', { signal }),
    refetchInterval: 15_000,
  })
  const items = (providers.data?.items ?? []).filter((source) => !source.remote_catalog)
  const counts = useSourceTorrentStats(items.length > 0)
  const visible = items.filter(
    (source) =>
      sourceMatches(filters.query, source.name, source.id, source.adapter) &&
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
        <dl className="grid gap-3 sm:grid-cols-3">
          <Statistic
            label="Running imports"
            value={overview.data.running}
            to="/runs?status=running"
          />
          <Statistic label="Queued imports" value={overview.data.queued} to="/runs?status=queued" />
          <Statistic label="Import capacity on this instance" value={overview.data.capacity} />
        </dl>
      )}
      {(items.length > 0 || filtered) && (
        <SourceFilters
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
      {providers.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="grid items-start gap-5 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((source) => (
            <section key={source.id} className="section-card">
              <h2 className="section-title">
                <Link
                  className="data-link"
                  to={`/providers/${encodeURIComponent(source.id)}`}
                  state={listState}
                >
                  {source.name || source.id}
                </Link>
              </h2>
              <SourceTorrentStats
                source={source.name || source.id}
                counts={counts.bySource.get(source.id)}
                unavailable={counts.isError}
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
  const location = useLocation()
  const legacyMatches =
    section === 'metadata' &&
    (new URLSearchParams(location.search).has('match_status') ||
      location.hash === '#matching-results')
  const query = useQuery({
    queryKey: ['aiosource'],
    queryFn: ({ signal }) => api<Status>('/aiosource', { signal }),
    refetchInterval: 5000,
  })
  const status = query.data
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
            {section === 'metadata' ? (
              <>
                <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                  <Statistic label="Film & series entries" value={status.status.works} />
                  {[
                    ['Identified torrents', 'matched', status.status.matched],
                    ['Needs review', 'ambiguous', status.status.ambiguous],
                    ['Not identified', 'unmatched', status.status.unmatched],
                    ['Awaiting identification', 'pending', status.status.matching_pending],
                  ].map(([label, decision, value]) => (
                    <Statistic
                      key={label}
                      label={String(label)}
                      value={Number(value)}
                      to={`/metadata/identification?${params({ match_status: decision })}`}
                    />
                  ))}
                  <Statistic label="Information pending" value={status.status.pending} />
                </dl>
                <div className="flex flex-wrap gap-2" aria-label="Current activity">
                  <Link
                    to="/metadata/history?kind=metadata"
                    aria-label="View information update history"
                  >
                    <Badge
                      variant={
                        status.syncing ? 'info' : status.status.sync_error ? 'warning' : 'secondary'
                      }
                    >
                      {status.syncing
                        ? 'Information update running'
                        : status.status.sync_error
                          ? 'Information update needs attention'
                          : 'Information update idle'}
                    </Badge>
                  </Link>
                  <Link
                    to="/metadata/history?kind=matching"
                    aria-label="View identification history"
                  >
                    <Badge variant={status.matching ? 'info' : 'secondary'}>
                      {status.matching ? 'Identification running' : 'Identification idle'}
                    </Badge>
                  </Link>
                </div>
                {status.rebuild_required && (
                  <p role="alert" className="notice notice-warning">
                    The change history gap exceeds 14 days. Reset the metadata catalog in{' '}
                    <Link className="underline" to="/metadata/settings#maintenance">
                      Settings
                    </Link>
                    .
                  </p>
                )}
              </>
            ) : (
              <>
                <dl className="grid gap-3 sm:grid-cols-3">
                  <Statistic
                    label="Configured services"
                    value={
                      Number(Boolean(status.config.alldebrid_secret)) +
                      Number(Boolean(status.config.torbox_secret))
                    }
                  />
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
            )}
          </>
        )
      )}
    </div>
  )
}
