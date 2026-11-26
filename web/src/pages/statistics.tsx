import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useLocation } from 'react-router-dom'
import { api, LIVE_REFRESH_INTERVAL, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { useListViewState } from '@/lib/detail-navigation'
import { routes } from '@/lib/routes'
import { filesResultsPath, sourceSettingsPath } from '@/lib/source-navigation'
import type { Items, ProviderSummary } from '@/lib/types'
import type { CollectionOverview } from '@/lib/collection-types'
import type { Controls, InformationStatus } from '@/lib/aiosource-types'
import { Empty, ErrorNotice, Loading, PageHeader, ResultCount } from '@/components/common'
import { ListFilters, useListFilters } from '@/components/list-filters'
import { compareListNames } from '@/lib/list-view'
import { SourceTorrentStats, useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Badge } from '@/components/ui/badge'
import { Disclosure } from '@/components/ui/disclosure'
import { Statistic } from '@/components/statistic'
import { Button } from '@/components/ui/button'
import { ArrowRightIcon } from '@phosphor-icons/react'
import { DebridStatistics } from '@/components/debrid-statistics'
import { number, useDate } from '@/lib/format'
import {
  bookSyncLabel,
  metadataSyncLabel,
  musicSyncLabel,
  softwareSyncLabel,
} from '@/lib/information-sync'

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
      filters.matchesItem(source.id, source.name, source.id, source.adapter) &&
      filters.matchesState(
        source.enabled ? 'enabled' : 'disabled',
        ...(!source.valid ? ['invalid'] : []),
      ),
  )
  const filtered = Boolean(filters.query || filters.selection || filters.state)
  // Keep existing bookmarks to advanced settings useful after separating views.
  if (new URLSearchParams(location.search).get('settings') === 'advanced')
    return (
      <Navigate
        to={`${routes.sources.indexers.torrents.settings}${location.search}${location.hash}`}
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
        <dl className="grid grid-cols-2 gap-3 xl:grid-cols-4">
          <Statistic
            label="Imported torrents"
            value={counts.totals ? counts.totals.imported : 'Not available'}
            loadingLabel={counts.isPending && !providers.isError ? 'Loading counts…' : undefined}
            to={filesResultsPath}
          />
          <Statistic
            label="Awaiting import completion"
            value={counts.totals ? counts.totals.awaiting_import : 'Not available'}
            loadingLabel={counts.isPending && !providers.isError ? 'Loading counts…' : undefined}
            to={routes.sources.indexers.torrents.history}
          />
          <Statistic
            label="Running imports"
            value={overview.data.source_running}
            to={`${routes.sources.indexers.torrents.history}?status=running`}
          />
          <Statistic
            label="Queued imports"
            value={overview.data.source_queued}
            to={`${routes.sources.indexers.torrents.history}?status=queued`}
          />
        </dl>
      )}
      {providers.data && (items.length > 0 || filtered) && (
        <ListFilters
          filters={filters}
          entries={items}
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
        <div className="grid items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((source) => (
            <section key={source.id} className="statistic-card flex h-full flex-col gap-4">
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
              <div className="card-actions mt-auto">
                <Button asChild variant="outline" size="sm">
                  <Link
                    to={`${routes.sources.indexers.torrents.history}?provider=${encodeURIComponent(source.id)}`}
                    state={listState}
                  >
                    <ArrowRightIcon aria-hidden="true" />
                    View history
                  </Link>
                </Button>
              </div>
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

export function AIOStatisticsPage({
  section,
}: {
  section: 'videos' | 'games' | 'software' | 'music' | 'books' | 'debrid'
}) {
  if (section === 'games') return <GameStatisticsPage />
  if (section === 'software') return <SoftwareStatisticsPage />
  if (section === 'music') return <MusicStatisticsPage />
  if (section === 'books') return <BookStatisticsPage />
  return section === 'debrid' ? <DebridStatisticsPage /> : <VideoStatisticsPage />
}

function VideoStatisticsPage() {
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
  const path = routes.metadata.catalogs.videos.base
  const running = Boolean(status?.syncing)
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === 'videos' || status.maintenance.scope === 'metadata')
  const automatic = status?.config.auto_sync
  const lastSync = status?.status.last_sync
  const appliedThrough = status?.status.applied_through
  const snapshot = status?.status.inventory_date
  const next = status?.automation.next_update
  const needsAttention = Boolean(
    status?.status.sync_error || status?.recovery_required || status?.status.recovery_started_at,
  )
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        status && (
          <>
            <dl className="grid grid-cols-2 gap-3">
              <Statistic label="Catalog entries" value={status.status.video_works} />
              <Statistic
                label="Information pending"
                value={status.status.pending}
                to={`${path}/history`}
              />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Synchronization"
                value={
                  resetting
                    ? 'Resetting information'
                    : metadataSyncLabel(status.status, running, automatic, status.recovery_required)
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
              <Statistic
                label="Export date"
                value={snapshot ? date(snapshot, 'date') : 'Not established'}
                size="sm"
              />
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
          </>
        )
      )}
    </div>
  )
}

function BookStatisticsPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: (query) =>
      query.state.data?.maintenance?.status === 'running'
        ? LIVE_REFRESH_INTERVAL()
        : COLLECTION_REFRESH_INTERVAL(),
  })
  const status = query.data
  const books = status?.books
  const path = routes.metadata.catalogs.books.base
  const running = Boolean(books?.running)
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === 'books' || status.maintenance.scope === 'metadata')
  const automatic = status?.config.openlibrary_enabled
  const lastSync = books?.last_sync
  const appliedThrough = books?.changes_through
  const snapshot = books?.snapshot_at
  const next = books?.next_update
  const needsAttention = Boolean(books?.error || books?.needs_export)
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        status && (
          <>
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Statistic label="Catalog entries" value={books?.works ?? 0} />
              <Statistic label="Editions" value={books?.editions ?? 0} />
              <Statistic label="Authors" value={books?.authors ?? 0} />
              <Statistic
                label="Information pending"
                value={books?.pending ?? 0}
                to={`${path}/history`}
              />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Synchronization"
                value={
                  resetting
                    ? 'Resetting information'
                    : books
                      ? bookSyncLabel(books, automatic)
                      : 'Not started'
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
              <Statistic
                label="Export date"
                value={snapshot ? date(snapshot, 'date') : 'Not established'}
                size="sm"
              />
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
            {books && books.position > 0 && (
              <Disclosure title="Import details">
                <dl className="grid gap-4 sm:grid-cols-3">
                  <div>
                    <dt className="text-sm text-muted-foreground">Records read from exports</dt>
                    <dd className="mt-1 font-medium tabular-nums">{number(books.position)}</dd>
                  </div>
                  <div>
                    <dt className="text-sm text-muted-foreground">Excluded by catalog filters</dt>
                    <dd className="mt-1 font-medium tabular-nums">{number(books.filtered ?? 0)}</dd>
                  </div>
                  <div>
                    <dt className="text-sm text-muted-foreground">Unreadable records</dt>
                    <dd className="mt-1 font-medium tabular-nums">{number(books.skipped)}</dd>
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
                <Link className="underline" to={`${routes.debrid.settings}#maintenance`}>
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

function GameStatisticsPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const status = query.data,
    games = status?.games
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === 'games' || status.maintenance.scope === 'metadata')
  const sync = resetting
    ? 'Resetting information'
    : games?.running
      ? 'Updating'
      : games?.error
        ? 'Needs attention'
        : games?.pending
          ? 'Update incomplete'
          : !games?.complete
            ? 'Initial import needed'
            : 'Up to date'
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        games && (
          <>
            <dl className="grid grid-cols-2 gap-3">
              <Statistic label="Catalog entries" value={games.works} />
              <Statistic
                label="Synchronization"
                value={sync}
                size="sm"
                to={routes.metadata.catalogs.games.settings}
              />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Last successful update"
                value={games.last_sync ? date(games.last_sync) : 'No completed update'}
                size="sm"
                to={routes.metadata.catalogs.games.history}
              />
              <Statistic
                label="Changes applied through"
                value={games.changes_through ? date(games.changes_through) : 'Not established'}
                size="sm"
                to={routes.metadata.catalogs.games.history}
              />
              <Statistic
                label="Next automatic update"
                value={
                  resetting
                    ? 'Waiting for reset'
                    : games.running
                      ? 'Running'
                      : !status?.config.igdb_enabled
                        ? 'Disabled'
                        : games.next_update
                          ? date(games.next_update)
                          : 'Waiting'
                }
                size="sm"
                to={routes.metadata.catalogs.games.settings}
              />
            </dl>
            <Disclosure title="Synchronization details">
              <dl className="grid gap-3 sm:grid-cols-2">
                <Statistic label="Records processed in current update" value={games.processed} />
                <Statistic
                  label="Last full reconciliation"
                  value={
                    games.last_reconciliation ? date(games.last_reconciliation) : 'Not established'
                  }
                  size="sm"
                />
              </dl>
            </Disclosure>
          </>
        )
      )}
    </div>
  )
}

function SoftwareStatisticsPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const status = query.data
  const software = status?.software
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === 'software' || status.maintenance.scope === 'metadata')
  const automatic = Boolean(status?.config.software_enabled)
  const needsAttention = Boolean(software?.error || software?.needs_full_import)
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        software && (
          <>
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Statistic label="Software applications" value={software.works} />
              <Statistic label="NVD CPE entries" value={software.nvd_entries} />
              <Statistic
                label="WinGet & Homebrew"
                value={software.winget_entries + software.homebrew_entries}
              />
              <Statistic label="Software icons" value={software.icons} />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Synchronization"
                value={resetting ? 'Resetting information' : softwareSyncLabel(software, automatic)}
                size="sm"
                to={routes.metadata.catalogs.software.settings}
              />
              <Statistic
                label="NVD update mode"
                value={
                  software.nvd_key_configured
                    ? 'Incremental API (API key)'
                    : 'Incremental API (public rate)'
                }
                size="sm"
                to={routes.metadata.catalogs.software.settings}
              />
              <Statistic
                label="Last successful update"
                value={software.last_sync ? date(software.last_sync) : 'No completed update'}
                size="sm"
                to={routes.metadata.catalogs.software.history}
              />
              <Statistic
                label="NVD changes applied through"
                value={
                  software.nvd_changes_through
                    ? date(software.nvd_changes_through)
                    : 'Not established'
                }
                size="sm"
                to={routes.metadata.catalogs.software.history}
              />
              <Statistic
                label="NVD snapshot date"
                value={
                  software.nvd_snapshot_at
                    ? date(software.nvd_snapshot_at, 'date')
                    : 'Not established'
                }
                size="sm"
              />
              <Statistic
                label="Next automatic update"
                value={
                  resetting
                    ? 'Waiting for reset'
                    : software.running
                      ? 'Running'
                      : !automatic
                        ? 'Disabled'
                        : software.next_update
                          ? date(software.next_update)
                          : 'Waiting'
                }
                size="sm"
                to={routes.metadata.catalogs.software.settings}
              />
            </dl>
            <div className="flex flex-wrap gap-2" aria-label="Current activity">
              <Link to={routes.metadata.catalogs.software.history} aria-label="View update history">
                <Badge
                  variant={
                    software.running || resetting
                      ? 'info'
                      : needsAttention
                        ? 'warning'
                        : 'secondary'
                  }
                >
                  {resetting
                    ? 'Reset in progress'
                    : software.running
                      ? 'Update running'
                      : needsAttention
                        ? 'Update needs attention'
                        : 'Update idle'}
                </Badge>
              </Link>
            </div>
            <Disclosure title="Import details">
              <dl className="grid gap-4 sm:grid-cols-3">
                <div>
                  <dt className="text-sm text-muted-foreground">WinGet packages</dt>
                  <dd className="mt-1 font-medium tabular-nums">
                    {number(software.winget_entries)}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Homebrew Casks</dt>
                  <dd className="mt-1 font-medium tabular-nums">
                    {number(software.homebrew_entries)}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Indexed selfh.st icons</dt>
                  <dd className="mt-1 font-medium tabular-nums">
                    {number(software.icons_indexed)}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Rows processed</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(software.position)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Excluded by catalog filters</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(software.filtered)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Skipped records</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(software.skipped)}</dd>
                </div>
              </dl>
            </Disclosure>
          </>
        )
      )}
    </div>
  )
}

function MusicStatisticsPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const status = query.data
  const music = status?.music
  const resetting =
    status?.maintenance?.status === 'running' &&
    (status.maintenance.scope === 'music' || status.maintenance.scope === 'metadata')
  const automatic = Boolean(status?.config.musicbrainz_enabled)
  const needsAttention = Boolean(music?.error || music?.needs_export || music?.schema_unsupported)
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        music && (
          <>
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Statistic label="Release groups" value={music.works} />
              <Statistic label="Releases" value={music.releases} />
              <Statistic label="Artists" value={music.artists} />
              <Statistic label="Cover Art Archive covers" value={music.covers} />
            </dl>
            <dl className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              <Statistic
                label="Synchronization"
                value={resetting ? 'Resetting information' : musicSyncLabel(music, automatic)}
                size="sm"
                to={routes.metadata.catalogs.music.settings}
              />
              <Statistic
                label="Update feed"
                value={
                  music.live_enabled
                    ? music.replication_sequence > 0
                      ? `Live Data Feed (#${number(music.replication_sequence)})`
                      : 'Live Data Feed (waiting for dump)'
                    : 'Full exports (CC0)'
                }
                size="sm"
                to={routes.metadata.catalogs.music.settings}
              />
              <Statistic
                label="Last successful update"
                value={music.last_sync ? date(music.last_sync) : 'No completed update'}
                size="sm"
                to={routes.metadata.catalogs.music.history}
              />
              <Statistic
                label="Changes applied through"
                value={music.changes_through ? date(music.changes_through) : 'Not established'}
                size="sm"
                to={routes.metadata.catalogs.music.history}
              />
              <Statistic
                label="Export date"
                value={music.snapshot_at ? date(music.snapshot_at, 'date') : 'Not established'}
                size="sm"
              />
              <Statistic
                label="Next automatic update"
                value={
                  resetting
                    ? 'Waiting for reset'
                    : music.running
                      ? 'Running'
                      : !automatic
                        ? 'Disabled'
                        : music.next_update
                          ? date(music.next_update)
                          : 'Waiting'
                }
                size="sm"
                to={routes.metadata.catalogs.music.settings}
              />
            </dl>
            <div className="flex flex-wrap gap-2" aria-label="Current activity">
              <Link to={routes.metadata.catalogs.music.history} aria-label="View update history">
                <Badge
                  variant={
                    music.running || resetting ? 'info' : needsAttention ? 'warning' : 'secondary'
                  }
                >
                  {resetting
                    ? 'Reset in progress'
                    : music.running
                      ? 'Update running'
                      : needsAttention
                        ? 'Update needs attention'
                        : 'Update idle'}
                </Badge>
              </Link>
            </div>
            <Disclosure title="Import details">
              <dl className="grid gap-4 sm:grid-cols-3">
                <div>
                  <dt className="text-sm text-muted-foreground">Export snapshot</dt>
                  <dd className="mt-1 font-medium mono">{music.snapshot || 'Not established'}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">MusicBrainz schema</dt>
                  <dd className="mt-1 font-medium tabular-nums">
                    {music.schema_sequence > 0 ? `#${music.schema_sequence}` : 'Not established'}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Labels</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(music.labels)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Rows read from exports</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(music.position)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Excluded by catalog filters</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(music.filtered)}</dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Unreadable records</dt>
                  <dd className="mt-1 font-medium tabular-nums">{number(music.skipped)}</dd>
                </div>
              </dl>
            </Disclosure>
          </>
        )
      )}
    </div>
  )
}
