import { ResultCount } from '@/components/common'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ArrowRightIcon } from '@phosphor-icons/react'
import { api, params, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { messageText, number, statusLabels, useDate } from '@/lib/format'
import { useURLPagination } from '@/lib/pagination'
import { resetListView } from '@/lib/list-view'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import type { Page } from '@/lib/types'
import type { OperationRun, OperationEvent, OperationStatus } from '@/lib/operation-types'
import { providerSteps, providerErrors } from '@/lib/provider-call'
import { DownloadAction } from '@/components/catalog-download'
import { Statistic } from '@/components/statistic'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'
import {
  ClearFiltersButton,
  ListEmpty,
  ErrorNotice,
  Loading,
  PageHeader,
  Pager,
  RunStatusBadge,
} from '@/components/common'

type Section = 'metadata' | 'debrid'
const statuses: Record<OperationStatus, string> = {
  running: statusLabels.running,
  succeeded: statusLabels.succeeded,
  failed: statusLabels.failed,
  cancelled: statusLabels.cancelled,
  interrupted: 'Interrupted',
}
const kinds = {
  metadata: 'Information update',
  matching: 'Torrent identification',
  cache: 'Cache verification',
  cleanup: 'Temporary magnet cleanup',
  download: 'Download preparation',
}
const services = {
  tmdb: 'TMDB',
  local: 'Local identification',
  alldebrid: 'AllDebrid',
  torbox: 'TorBox',
}
const triggers = { manual: 'Manual', automatic: 'Automatic', playback: 'Playback request' }
const phases: Record<string, string> = {
  ...providerSteps,

  configuration: 'Starting',
  changes: 'Downloading information changes',
  inventory: 'Importing film & series inventory',
  details: 'Updating film & series information',
  identification: 'Identifying torrents',
  availability: 'Verifying cache availability',
  cleanup: 'Cleaning temporary magnets',
  storage: 'Saving information',
  download: 'Preparing download',
  waiting: 'Waiting for debrid',
  ready: 'Download ready',
  succeeded: statusLabels.succeeded,
  failed: statusLabels.failed,
  cancelled: statusLabels.cancelled,
  interrupted: 'Interrupted',
}
const resultLabels: Record<string, string> = {
  matched: 'Identified',
  ambiguous: 'Needs review',
  unmatched: 'Not identified',
  cached: 'Cached',
  uncached: 'Not cached',
  cleaned: 'Cleaned up',
}

function OperationBadge({ run }: { run: OperationRun }) {
  return run.kind === 'download' && run.phase === 'waiting' && run.status === 'succeeded' ? (
    <Badge variant="info">Preparing</Badge>
  ) : run.status === 'interrupted' ? (
    <Badge variant="warning">Interrupted</Badge>
  ) : (
    <RunStatusBadge run={{ status: run.status, cancel_requested: false, pause_requested: false }} />
  )
}

export function OperationHistoryPage({ section }: { section: Section }) {
  const listState = useListViewState()
  const [search, setSearch] = useViewSearchParams()
  const pagination = useURLPagination()
  const { limit, offset } = pagination
  const kind = search.get('kind') || ''
  const service = section === 'metadata' ? '' : search.get('service') || ''
  const status = search.get('status') || ''
  const date = useDate()
  const query = useQuery({
    queryKey: ['operation-history', section, kind, service, status, offset, limit],
    queryFn: ({ signal }) =>
      api<Page<OperationRun>>(
        `/operations?${params({ group: section, kind, service, status, offset, limit })}`,
        { signal },
      ),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  function filter(key: string, value: string) {
    const next = new URLSearchParams(search)
    value ? next.set(key, value) : next.delete(key)
    if (key !== 'offset') next.delete('offset')
    setSearch(next)
  }
  return (
    <div className="page">
      <div className="toolbar">
        <div className="field">
          <Label htmlFor="operation-type" className="sr-only">
            {section === 'metadata' ? 'Filter by operation' : 'Filter by service'}
          </Label>
          {section === 'metadata' ? (
            <select
              id="operation-type"
              className="native-select min-w-48"
              value={kind}
              onChange={(e) => filter('kind', e.target.value)}
            >
              <option value="">All operations</option>
              <option value="metadata">Information updates</option>
              <option value="matching">Torrent identification</option>
            </select>
          ) : (
            <select
              id="operation-type"
              className="native-select min-w-48"
              value={service}
              onChange={(e) => filter('service', e.target.value)}
            >
              <option value="">All services</option>
              <option value="alldebrid">AllDebrid</option>
              <option value="torbox">TorBox</option>
            </select>
          )}
        </div>
        {section === 'debrid' && (
          <div className="field">
            <Label htmlFor="debrid-operation-kind" className="sr-only">
              Filter by operation
            </Label>
            <select
              id="debrid-operation-kind"
              className="native-select"
              value={kind}
              onChange={(event) => filter('kind', event.target.value)}
            >
              <option value="">All operations</option>
              <option value="cache">Cache verifications</option>
              <option value="download">Download preparations</option>
              <option value="cleanup">Temporary magnet cleanup</option>
            </select>
          </div>
        )}
        <div className="field">
          <Label htmlFor="operation-status" className="sr-only">
            Filter by status
          </Label>
          <select
            id="operation-status"
            className="native-select"
            value={status}
            onChange={(e) => filter('status', e.target.value)}
          >
            <option value="">All statuses</option>
            {Object.entries(statuses).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <ClearFiltersButton
          active={Boolean(kind || service || status)}
          onClear={() => setSearch(resetListView(search, ['kind', 'service', 'status']))}
        />
      </div>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <>
            <ResultCount total={query.data.total} label="operations" />
            {query.data.items?.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Operation / run</TableHead>
                      <TableHead>Service / trigger</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="text-right">Records</TableHead>
                      <TableHead className="text-right">Errors</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>
                        <span className="sr-only">Open</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {query.data.items.map((run) => (
                      <TableRow key={run.id}>
                        <TableCell>
                          <Link
                            className="data-link font-medium"
                            to={`/${section}/history/${encodeURIComponent(run.id)}`}
                            state={listState}
                          >
                            {kinds[run.kind]}
                          </Link>
                          <p className="mono mt-1 text-muted-foreground">{run.id.slice(0, 8)}</p>
                          {run.resource && (
                            <p className="mt-1 text-xs text-muted-foreground">{run.resource}</p>
                          )}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {services[run.service]}
                          <p className="mt-1">{triggers[run.trigger]}</p>
                        </TableCell>
                        <TableCell className="whitespace-normal">
                          <OperationBadge run={run} />
                          {run.error && (
                            <p
                              className={`mt-2 max-w-sm text-xs leading-5 ${run.status === 'failed' ? 'text-destructive' : 'text-amber-300'}`}
                            >
                              {messageText(run.error)}
                            </p>
                          )}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs">
                          {number(run.processed)}
                        </TableCell>
                        <TableCell className="text-right font-mono text-xs">
                          {number(run.errors)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(run.created_at)}
                        </TableCell>
                        <TableCell>
                          <Button variant="ghost" size="icon-sm" asChild>
                            <Link
                              to={`/${section}/history/${encodeURIComponent(run.id)}`}
                              state={listState}
                              aria-label={`Open run ${run.id}`}
                            >
                              <ArrowRightIcon />
                            </Link>
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <ListEmpty
                title="No runs in this view"
                description="New operations will appear here. History starts when this version is installed; earlier activity remains in Logs."
                filtered={Boolean(kind || service || status)}
                offset={offset}
                onFirstPage={() => setSearch(resetListView(search))}
              />
            )}
            <Pager
              total={query.data.total}
              limit={query.data.limit}
              offset={query.data.offset}
              busy={query.isFetching}
              onChange={pagination.setOffset}
            />
          </>
        )
      )}
    </div>
  )
}

export function OperationDetailPage({ section }: { section: Section }) {
  const { id } = useParams()
  const pagination = useURLPagination()
  const { limit, offset } = pagination
  const date = useDate()
  const run = useQuery({
    queryKey: ['operation', id],
    queryFn: ({ signal }) =>
      api<OperationRun>(`/operations/${encodeURIComponent(id!)}`, { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const events = useQuery({
    queryKey: ['operation-events', id, offset, limit],
    queryFn: ({ signal }) =>
      api<Page<OperationEvent>>(
        `/operations/${encodeURIComponent(id!)}/events?${params({ offset, limit })}`,
        { signal },
      ),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const result = run.data
  return (
    <div className="page">
      <PageHeader
        title={result ? kinds[result.kind] : 'Operation'}
        back={{ to: `/${section}/history`, label: 'History' }}
        description="Inspect the status, saved progress and events for this operation."
      />
      <ErrorNotice error={run.error} retry={() => void run.refetch()} />
      {run.isPending ? (
        <Loading />
      ) : (
        result && (
          <>
            <div className="flex flex-wrap items-center gap-3">
              <OperationBadge run={result} />
              <span className="text-sm text-muted-foreground">
                {services[result.service]} · {triggers[result.trigger]}
              </span>
              <span className="mono text-muted-foreground">{result.id}</span>
            </div>
            {result.error && (
              <p
                role={result.status === 'failed' ? 'alert' : 'status'}
                className={`notice ${result.status === 'failed' ? 'notice-error' : 'notice-warning'}`}
              >
                {messageText(result.error)}
              </p>
            )}
            {result.download && (
              <section className="section-card" aria-label="Download">
                <h2 className="section-title">Download</h2>
                <DownloadAction
                  key={result.id}
                  workID={String(result.download.work_id)}
                  selection={result.download.input}
                  service={{
                    service: result.download.input.service,
                    state: 'unknown',
                    can_add: false,
                  }}
                  release={{
                    occurrence_id: result.download.input.occurrence_id,
                    fingerprint: result.download.input.fingerprint,
                    title: '',
                    provider: '',
                    size: 0,
                    languages: [],
                    services: [],
                  }}
                  operationID={result.id}
                  preparing={result.phase === 'waiting' && result.status === 'succeeded'}
                />
              </section>
            )}
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              {Object.entries({
                'Records processed': result.processed,
                Errors: result.errors,
                Created: date(result.created_at),
                Finished: result.finished_at ? date(result.finished_at) : 'In progress',
                'Last recorded step': phases[result.phase] || result.phase,
                'Last checkpoint': date(result.updated_at),
                ...Object.fromEntries(
                  Object.entries(result.results).map(([key, value]) => [
                    resultLabels[key] || key,
                    value,
                  ]),
                ),
                ...(result.resource ? { Resource: result.resource } : {}),
              }).map(([label, value]) => (
                <Statistic
                  key={label}
                  label={label}
                  value={value}
                  size={typeof value === 'number' ? 'default' : 'sm'}
                />
              ))}
            </dl>
            <p className="help">
              Counts show the last recorded progress for this attempt. Unidentified torrents and
              uncached torrents are results, not errors. Stopping or restarting preserves saved
              data; pending work follows the existing automatic retry settings.
            </p>
            {result.kind === 'metadata' && result.errors > 0 && result.status === 'succeeded' && (
              <p className="notice notice-warning" role="status">
                The pass completed with deferred work. Failed information requests remain available
                for a later retry.
              </p>
            )}
            <section className="space-y-4" aria-label="Operation events">
              <h2 className="section-title">Events</h2>
              <ErrorNotice error={events.error} retry={() => void events.refetch()} />
              {events.isPending ? (
                <Loading />
              ) : (
                events.data && (
                  <>
                    <ResultCount total={events.data.total} label="events" />
                    {events.data.items?.length ? (
                      <div className="table-frame">
                        <Table>
                          <TableHeader>
                            <TableRow>
                              <TableHead>Time</TableHead>
                              <TableHead>Step</TableHead>
                              <TableHead className="text-right">Records</TableHead>
                              <TableHead className="text-right">Errors</TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody>
                            {events.data.items?.map((event) => (
                              <TableRow key={event.id}>
                                <TableCell className="text-xs text-muted-foreground">
                                  {date(event.created_at)}
                                </TableCell>
                                <TableCell>
                                  {phases[event.phase] || event.phase}
                                  {event.phase.startsWith('provider_') && (
                                    <p className="help mt-1">
                                      {number(event.duration_ms || 0)} ms
                                      {event.http_status ? ` · HTTP ${event.http_status}` : ''}
                                      {event.error_code
                                        ? ` · ${providerErrors[event.error_code] ?? 'Provider error'}`
                                        : ''}
                                    </p>
                                  )}
                                </TableCell>
                                <TableCell className="text-right font-mono text-xs">
                                  {number(event.processed)}
                                </TableCell>
                                <TableCell className="text-right font-mono text-xs">
                                  {number(event.errors)}
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </div>
                    ) : (
                      <ListEmpty
                        title={offset ? 'No events on this page' : 'No events recorded yet'}
                        description="Events appear as progress is recorded. Older operations may have no retained events."
                        offset={offset}
                        onFirstPage={() => pagination.setOffset(0)}
                      />
                    )}
                    <Pager
                      total={events.data.total}
                      limit={events.data.limit}
                      offset={events.data.offset}
                      busy={events.isFetching}
                      onChange={pagination.setOffset}
                    />
                  </>
                )
              )}
            </section>
          </>
        )
      )}
    </div>
  )
}
