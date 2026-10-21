import { ResultCount } from '@/components/common'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ArrowRightIcon } from '@phosphor-icons/react'
import { api, params, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { useSourceNames } from '@/lib/source-names'
import { number, bytes, percentage, useDate } from '@/lib/format'
import { useURLPagination } from '@/lib/pagination'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import { resetListView } from '@/lib/list-view'
import type { Page } from '@/lib/types'
import type { StremioRequest, StremioStatistics } from '@/lib/stremio-types'
import { providerSteps, providerErrors } from '@/lib/provider-call'
import { Button } from '@/components/ui/button'
import { Statistic } from '@/components/statistic'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  ClearFiltersButton,
  Empty,
  ErrorNotice,
  Loading,
  ListEmpty,
  PageHeader,
  Pager,
} from '@/components/common'

const origins = { private: 'Private installation', public: 'Public installation' } as const
const kinds = { stream: 'Streams', manifest: 'Manifest', playback: 'Playback' } as const
const outcomes = {
  results: 'Results returned',
  empty: 'No results',
  invalid: 'Invalid resource',
  failed: 'Request failed',
  cancelled: 'Interrupted',
  busy: 'Too many active requests',
  pending: 'Playback pending',
} as const
const services = { direct: 'Direct', alldebrid: 'AllDebrid', torbox: 'TorBox' } as const
const cacheLabels = {
  direct: 'Direct',
  cached: 'Cached',
  download: 'Download required',
  unknown: 'Unknown',
} as const

function Outcome({ item }: { item: StremioRequest }) {
  return (
    <Badge
      variant={
        item.outcome === 'results'
          ? 'success'
          : item.outcome === 'failed'
            ? 'destructive'
            : item.outcome === 'empty' ||
                item.outcome === 'invalid' ||
                item.outcome === 'busy' ||
                item.outcome === 'pending'
              ? 'warning'
              : 'secondary'
      }
    >
      {item.outcome === 'results' && item.kind !== 'stream' ? 'Completed' : outcomes[item.outcome]}
    </Badge>
  )
}

export function StremioStatisticsPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['stremio-statistics'],
    queryFn: ({ signal }) => api<StremioStatistics>('/stremio/statistics', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading statistics" />
      ) : (
        query.data && (
          <>
            <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <Statistic label="Requests" value={query.data.requests} to="/addon/history" />
              <Statistic
                label="Results returned"
                value={query.data.results}
                to="/addon/history?status=results"
              />
              <Statistic
                label="Requests without results"
                value={query.data.empty}
                detail={`(${percentage(query.data.empty, query.data.requests)})`}
                to="/addon/history?status=empty"
              />
              <Statistic
                label="Failed or interrupted requests"
                value={query.data.failed}
                detail={`(${percentage(query.data.failed, query.data.requests)})`}
                to="/addon/history"
              />
              <Statistic label="Average response time (ms)" value={query.data.average_ms} />
            </dl>
            <p className="help">
              Retained history: last 7 days, up to 10,000 requests.
              {query.data.since && ` Oldest request: ${date(query.data.since)}.`}
            </p>
            {query.data.requests === 0 && (
              <Empty
                title="No Stremio requests yet"
                description="Requests appear when Stremio or AIOStreams asks this instance for streams."
              />
            )}
          </>
        )
      )}
    </div>
  )
}

export function StremioHistoryPage() {
  const date = useDate(),
    listState = useListViewState()
  const [search, setSearch] = useViewSearchParams()
  const q = search.get('q') ?? '',
    status = search.get('status') ?? '',
    origin = search.get('origin') ?? '',
    kind = search.get('kind') ?? ''
  const pagination = useURLPagination(),
    { limit, offset } = pagination
  const query = useQuery({
    queryKey: ['stremio-requests', { q, status, origin, kind, limit, offset }],
    queryFn: ({ signal }) =>
      api<Page<StremioRequest>>(
        `/stremio/requests?${params({ q, status, origin, kind, limit, offset })}`,
        {
          signal,
        },
      ),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  function filter(patch: Record<string, string>) {
    const next = new URLSearchParams(search)
    next.delete('offset')
    for (const [key, value] of Object.entries(patch))
      value ? next.set(key, value) : next.delete(key)
    setSearch(next)
  }
  return (
    <div className="page">
      <form
        className="toolbar rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          filter({ q: String(new FormData(event.currentTarget).get('q') ?? '').trim() })
        }}
      >
        <div className="field min-w-0 basis-full sm:basis-auto sm:flex-1">
          <Label htmlFor="stremio-resource">Resource identifier</Label>
          <Input
            id="stremio-resource"
            name="q"
            key={q}
            defaultValue={q}
            placeholder="IMDb, TMDB, TVDB or episode"
          />
        </div>
        <div className="field">
          <Label htmlFor="stremio-outcome">Result</Label>
          <select
            className="native-select"
            id="stremio-outcome"
            value={status}
            onChange={(event) => filter({ status: event.target.value })}
          >
            <option value="">All results</option>
            {Object.entries(outcomes).map(([value, label]) => (
              <option value={value} key={value}>
                {label}
              </option>
            ))}
          </select>
        </div>
        {[
          { name: 'origin', label: 'Installation', value: origin, choices: origins },
          { name: 'kind', label: 'Request type', value: kind, choices: kinds },
        ].map((item) => (
          <div className="field" key={item.name}>
            <Label htmlFor={`stremio-${item.name}`}>{item.label}</Label>
            <select
              id={`stremio-${item.name}`}
              className="native-select"
              value={item.value}
              onChange={(e) => filter({ [item.name]: e.target.value })}
            >
              <option value="">All</option>
              {Object.entries(item.choices).map(([value, label]) => (
                <option value={value} key={value}>
                  {label}
                </option>
              ))}
            </select>
          </div>
        ))}
        <Button variant="outline" type="submit" className="self-end">
          Search
        </Button>
        <div className="self-end">
          <ClearFiltersButton
            active={Boolean(q || status || origin || kind)}
            onClear={() => setSearch(resetListView(search, ['q', 'status', 'origin', 'kind']))}
          />
        </div>
      </form>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <>
            <ResultCount total={query.data.total} label="requests" />
            {query.data.items?.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Request</TableHead>
                      <TableHead>Result</TableHead>
                      <TableHead className="text-right">Returned</TableHead>
                      <TableHead className="text-right">Response time</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>
                        <span className="sr-only">Open</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {query.data.items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <Link
                            className="data-link mono"
                            to={`/addon/history/${item.id}`}
                            state={listState}
                          >
                            {item.resource}
                          </Link>
                          <p className="help mt-1">
                            {kinds[item.kind]} · {origins[item.origin]} · #{item.id}
                            {item.service && ` · ${services[item.service]}`}
                          </p>
                        </TableCell>
                        <TableCell>
                          <Outcome item={item} />
                        </TableCell>
                        <TableCell className="mono text-right">
                          {item.kind === 'stream' ? number(item.returned) : '—'}
                        </TableCell>
                        <TableCell className="mono text-right">
                          {number(item.duration_ms)} ms
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(item.created_at)}
                        </TableCell>
                        <TableCell>
                          <Button variant="ghost" size="icon-sm" asChild>
                            <Link
                              to={`/addon/history/${item.id}`}
                              state={listState}
                              aria-label={`Open request ${item.id}`}
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
                title="No requests in this view"
                description="Requests are recorded when the addon is used."
                filtered={Boolean(q || status || origin || kind)}
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

export function StremioRequestPage() {
  const sourceName = useSourceNames()
  const { id = '' } = useParams(),
    date = useDate()
  const query = useQuery({
    queryKey: ['stremio-request', id],
    queryFn: ({ signal }) =>
      api<StremioRequest>(`/stremio/requests/${encodeURIComponent(id)}`, { signal }),
    refetchInterval: false,
    staleTime: Infinity,
  })
  const item = query.data
  return (
    <div className="page">
      <PageHeader
        title={item ? `Request #${item.id}` : 'Request details'}
        description={
          item
            ? `${kinds[item.kind]} · ${origins[item.origin]} · ${item.resource} · ${date(item.created_at)}`
            : undefined
        }
        back={{ to: '/addon/history', label: 'Stremio history' }}
        actions={item && <Outcome item={item} />}
      />
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading />
      ) : (
        item && (
          <>
            {item.kind !== 'stream' ? (
              <section className="section-card">
                <h2 className="section-title">Request details</h2>
                <p className="help">
                  {number(item.duration_ms)} ms · {origins[item.origin]}
                  {item.service && ` · ${services[item.service]}`}
                </p>
                <p>
                  {item.outcome === 'results'
                    ? item.kind === 'playback'
                      ? 'Playback link resolved. The link is not stored in history.'
                      : 'Manifest returned.'
                    : item.outcome === 'pending'
                      ? 'The selected torrent is not ready for playback. Try again shortly.'
                      : 'The request could not complete. Check the related service entries in Logs.'}
                </p>
              </section>
            ) : (
              <>
                <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                  <Statistic label="Identified source records" value={item.matched} />
                  <Statistic label="Eligible torrents" value={item.eligible} />
                  <Statistic label="Results returned" value={item.returned} />
                  <Statistic label="Response time (ms)" value={item.duration_ms} />
                </dl>
                <section className="section-card">
                  <h2 className="section-title">Selection details</h2>
                  <dl className="grid gap-4 sm:grid-cols-2">
                    <div>
                      <dt className="help">Invalid or missing info hash</dt>
                      <dd className="mt-1 tabular-nums">{number(item.invalid_hash)}</dd>
                    </div>
                    <div>
                      <dt className="help">Excluded by language filter</dt>
                      <dd className="mt-1 tabular-nums">{number(item.language_excluded ?? 0)}</dd>
                    </div>
                    <div>
                      <dt className="help">Different episode</dt>
                      <dd className="mt-1 tabular-nums">{number(item.wrong_episode)}</dd>
                    </div>
                    <div>
                      <dt className="help">Private source unavailable for playback</dt>
                      <dd className="mt-1 tabular-nums">{number(item.private_unavailable)}</dd>
                    </div>
                    <div>
                      <dt className="help">No matching video in provider files</dt>
                      <dd className="mt-1 tabular-nums">{number(item.file_unavailable)}</dd>
                    </div>
                  </dl>
                  <p className="help">
                    A torrent can produce one result per configured debrid service. Provider
                    exclusions are counted separately.
                  </p>
                  {item.outcome === 'empty' && (
                    <p className="notice notice-warning">
                      {item.matched === 0
                        ? 'No current identification was published for this resource.'
                        : (item.language_excluded ?? 0) > 0
                          ? 'The installed language filter excluded source records. Disable it or include more languages in the installation link.'
                          : item.eligible === 0
                            ? 'All source records were excluded by their info hash or episode.'
                            : 'The configured playback services returned no eligible results.'}
                    </p>
                  )}
                  {item.outcome === 'failed' && (
                    <p className="notice notice-error">
                      The request could not complete. Check the related service entries in Logs.
                    </p>
                  )}
                </section>
                <section className="space-y-4">
                  <h2 className="section-title">Returned results</h2>
                  <ResultCount total={item.results?.length ?? 0} label="saved results" />
                  {item.results?.length ? (
                    <div className="table-frame">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>Result / source</TableHead>
                            <TableHead>Service / cache</TableHead>
                            <TableHead>File</TableHead>
                            <TableHead className="text-right">Size</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {item.results.map((result, index) => (
                            <TableRow key={index}>
                              <TableCell className="max-w-lg whitespace-normal">
                                <p className="font-medium">{result.name}</p>
                                <p className="mt-1 whitespace-pre-wrap break-words text-sm">
                                  {result.title}
                                </p>
                                {result.source_id ? (
                                  <Link
                                    className="data-link mt-2 inline-block text-xs"
                                    to={`/torrents?${params({ provider: result.provider, q: result.source_id })}`}
                                  >
                                    {sourceName(result.provider, result.provider_name)} ·{' '}
                                    {result.source_id}
                                  </Link>
                                ) : (
                                  <p className="help mt-2">
                                    {sourceName(result.provider, result.provider_name)}
                                  </p>
                                )}
                              </TableCell>
                              <TableCell>
                                <p className="mb-1 text-sm">{services[result.service]}</p>
                                <Badge
                                  variant={
                                    result.cache === 'cached'
                                      ? 'success'
                                      : result.cache === 'unknown' || result.cache === 'download'
                                        ? 'warning'
                                        : 'secondary'
                                  }
                                >
                                  {cacheLabels[result.cache]}
                                </Badge>
                              </TableCell>
                              <TableCell className="max-w-md whitespace-normal break-words text-xs">
                                {result.filename}
                              </TableCell>
                              <TableCell className="text-right text-xs">
                                {bytes(result.size)}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  ) : (
                    <Empty
                      title="No results returned"
                      description="This request did not return any playable result."
                    />
                  )}
                  {item.truncated && (
                    <p className="notice notice-warning">
                      This request returned more than 1,000 results. The first 1,000 display
                      summaries were retained; the total count above is complete.
                    </p>
                  )}
                </section>
              </>
            )}
            {Boolean(item.calls?.length) && (
              <section className="space-y-4" aria-label="Provider calls">
                <h2 className="section-title">Provider calls</h2>
                <ResultCount total={item.calls?.length ?? 0} label="calls" />
                <div className="table-frame">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Step</TableHead>
                        <TableHead>Result</TableHead>
                        <TableHead className="text-right">Response time</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {item.calls!.map((call, index) => (
                        <TableRow key={index}>
                          <TableCell>{providerSteps[call.phase] ?? 'Provider request'}</TableCell>
                          <TableCell>
                            <Badge variant={call.error_code ? 'warning' : 'success'}>
                              {call.error_code
                                ? (providerErrors[call.error_code] ?? 'Provider error')
                                : 'Completed'}
                            </Badge>
                            {call.http_status > 0 && (
                              <p className="help mt-1">HTTP {call.http_status}</p>
                            )}
                          </TableCell>
                          <TableCell className="mono text-right">
                            {number(call.duration_ms)} ms
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </section>
            )}
          </>
        )
      )}
    </div>
  )
}
