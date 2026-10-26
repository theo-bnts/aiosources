import { ResultCount } from '@/components/common'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import {
  ArrowRightIcon,
  DownloadSimpleIcon,
  MagnifyingGlassIcon,
  FileTextIcon,
} from '@phosphor-icons/react'
import { api, ApiError, AUTO_REFRESH_INTERVAL, downloadBytes, params, rawPage } from '@/lib/api'
import { useSourceNames } from '@/lib/source-names'
import type { Page, RawDetail, RawRecord, Torrent } from '@/lib/types'
import { messageText, useDate, number } from '@/lib/format'
import { useURLPagination } from '@/lib/pagination'
import { resetListView } from '@/lib/list-view'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'
import { CodeEditor } from '@/components/code-editor'
import {
  ClearFiltersButton,
  ListEmpty,
  ErrorNotice,
  Loading,
  PageHeader,
  Pager,
} from '@/components/common'
import { TorrentSearchForm } from '@/components/torrent-filters'
import { OccurrenceDialog, TorrentTable } from '@/components/torrent-history'
import { readTorrentFilters, torrentFilterParams, type TorrentFilters } from '@/lib/search-types'
import { decodeSearchJSON, exactField, exactJSON } from '@/lib/search-json'

function ProcessedRecordFilters({
  q,
  processing,
  onFilter,
  onProcessingChange,
  onClear,
}: {
  q: string
  processing: string
  onFilter: (q: string) => void
  onProcessingChange: (value: string) => void
  onClear: () => void
}) {
  return (
    <form
      className="space-y-4 rounded-md border border-border p-4"
      aria-label="Processed record filters"
      onSubmit={(event) => {
        event.preventDefault()
        onFilter(String(new FormData(event.currentTarget).get('q') ?? '').trim())
      }}
    >
      <div className="field">
        <Label htmlFor="raw-search">Search text</Label>
        <Input
          key={q}
          id="raw-search"
          name="q"
          defaultValue={q}
          placeholder="Search record data…"
        />
      </div>
      <Disclosure title="Filters" open>
        <div className="field max-w-sm">
          <Label htmlFor="run-raw-processing">Processing result</Label>
          <select
            id="run-raw-processing"
            className="native-select w-full"
            value={processing}
            onChange={(event) => onProcessingChange(event.target.value)}
          >
            <option value="">All results</option>
            <option value="error">Errors</option>
            <option value="ignored">Ignored</option>
            <option value="enrichment">Enrichment</option>
            <option value="accepted">Accepted</option>
          </select>
        </div>
      </Disclosure>
      <div className="flex flex-wrap gap-2">
        <Button id="raw-submit" type="submit">
          <MagnifyingGlassIcon />
          Search
        </Button>
        <ClearFiltersButton onClear={onClear} />
      </div>
    </form>
  )
}

function DownloadButton({
  path,
  filename,
  label,
}: {
  path: string
  filename: string
  label: string
}) {
  const download = useMutation({ mutationFn: () => downloadBytes(path, filename) })
  return (
    <div className="space-y-2">
      <Button variant="outline" disabled={download.isPending} onClick={() => download.mutate()}>
        <DownloadSimpleIcon />
        {download.isPending ? 'Downloading…' : label}
      </Button>
      <ErrorNotice error={download.error} scope="action" />
    </div>
  )
}

export function TorrentsPage() {
  const returnFocus = useRef<HTMLElement | null>(null)
  const [search, setSearch] = useViewSearchParams()
  const filters = readTorrentFilters(search)
  const filterKey = torrentFilterParams(filters).toString()
  const explicitLimit = search.get('limit')
  const pagination = useURLPagination()
  const { limit, offset } = pagination
  const occurrenceId = search.get('occurrence') ?? ''
  const [selected, setSelected] = useState<Torrent | null>(null)
  const query = torrentFilterParams(filters)
  query.set('limit', String(limit))
  query.set('offset', String(offset))
  const torrents = useQuery({
    queryKey: ['torrents', query.toString()],
    queryFn: ({ signal }) =>
      api<Page<Torrent>>(`/torrents?${query}`, { signal }, decodeSearchJSON<Page<Torrent>>),
  })
  const current =
    torrents.data?.items?.find((item) => item.occurrence_id === occurrenceId) ??
    (selected?.occurrence_id === occurrenceId ? selected : undefined)
  const hasFilters = Object.entries(filters).some(([key, value]) =>
    key === 'sort' ? value !== 'recent' : Array.isArray(value) ? value.length > 0 : Boolean(value),
  )
  function applyFilters(nextFilters: TorrentFilters) {
    const next = torrentFilterParams(nextFilters)
    if (explicitLimit) next.set('limit', explicitLimit)
    // Commit the applied form before accepting a new draft. A delayed navigation
    // must not replace text entered while the previous results are still loading.
    setSearch(next, { flushSync: true })
  }
  function openOccurrence(torrent: Torrent, target: HTMLElement) {
    if (!occurrenceId) returnFocus.current = target
    setSelected(torrent)
    const next = new URLSearchParams(search)
    next.set('occurrence', torrent.occurrence_id)
    next.delete('occurrence_tab')
    next.delete('history_offset')
    next.delete('related_offset')
    setSearch(next)
  }
  return (
    <div className="page">
      <TorrentSearchForm key={filterKey} filters={filters} onApply={applyFilters} />
      <ErrorNotice error={torrents.error} retry={() => void torrents.refetch()} />
      {torrents.isPending ? (
        <Loading label="Searching published occurrences" />
      ) : (
        torrents.data && (
          <>
            <ResultCount total={torrents.data.total} label="torrents" />
            {torrents.data.items?.length ? (
              <TorrentTable items={torrents.data.items} onOpen={openOccurrence} />
            ) : (
              <ListEmpty
                title={offset ? 'No torrents on this page' : 'No torrents in this view'}
                description="Import torrents from a source to search them here. Previews do not add published torrents."
                filtered={hasFilters}
                offset={offset}
                onFirstPage={() => setSearch(resetListView(search))}
                action={
                  hasFilters ? (
                    <ClearFiltersButton onClear={() => applyFilters({})} />
                  ) : (
                    <Button variant="outline" asChild>
                      <Link to="/providers/settings">Choose a source</Link>
                    </Button>
                  )
                }
              />
            )}
            <Pager
              total={torrents.data.total}
              limit={torrents.data.limit}
              offset={torrents.data.offset}
              busy={torrents.isFetching}
              onChange={pagination.setOffset}
            />
          </>
        )
      )}
      {occurrenceId && (
        <OccurrenceDialog
          key={occurrenceId}
          id={occurrenceId}
          torrent={current}
          onOpen={openOccurrence}
          onClose={() => {
            const next = new URLSearchParams(search)
            for (const key of [
              'occurrence',
              'occurrence_tab',
              'history_offset',
              'history_limit',
              'related_offset',
              'related_limit',
            ])
              next.delete(key)
            setSearch(next)
            setSelected(null)
          }}
          onReturnFocus={() => {
            if (returnFocus.current?.isConnected) returnFocus.current.focus()
            else document.getElementById('torrent-search')?.focus()
          }}
        />
      )}
    </div>
  )
}

export function RawTableView({ runId, compact = false }: { runId: string; compact?: boolean }) {
  const listState = useListViewState()
  const date = useDate()
  const [search, setSearch] = useViewSearchParams()
  const statusKey = 'raw_status'
  const processing = search.get(statusKey) ?? ''
  const qKey = 'raw_q'
  const offsetKey = 'raw_offset'
  const q = search.get(qKey) ?? ''
  const pagination = useURLPagination({ offsetKey, limitKey: 'raw_limit' })
  const { limit, offset } = pagination
  const run = runId
  const records = useQuery({
    queryKey: ['raw', { run, q, status: processing, offset, limit }],
    queryFn: ({ signal }) =>
      api<Page<RawRecord> & { counts: Record<string, number> }>(
        `/raw?${params({ run, q, status: processing, limit, offset })}`,
        { signal },
        decodeSearchJSON<Page<RawRecord> & { counts: Record<string, number> }>,
      ),
  })
  return (
    <div className="space-y-5">
      <ProcessedRecordFilters
        q={q}
        processing={processing}
        onClear={() =>
          setSearch(resetListView(search, [qKey, statusKey], offsetKey), { flushSync: true })
        }
        onFilter={(nextQ) => {
          const next = new URLSearchParams(search)
          next.delete(offsetKey)
          nextQ ? next.set(qKey, nextQ) : next.delete(qKey)
          setSearch(next, { flushSync: true })
        }}
        onProcessingChange={(value) => {
          const next = resetListView(search, [], offsetKey)
          value ? next.set(statusKey, value) : next.delete(statusKey)
          setSearch(next, { flushSync: true })
        }}
      />
      <div className="space-y-3">
        {compact && (
          <p className="help">
            Storage saving retains record errors here. Successful records are omitted; response
            errors remain in Events, and imported torrents remain in Search.
          </p>
        )}
        {records.data?.counts && !compact && (
          <p className="help" role="status">
            {number(records.data.counts.error)}{' '}
            {records.data.counts.error === 1 ? 'error' : 'errors'} ·{' '}
            {number(records.data.counts.ignored)} ignored · {number(records.data.counts.enrichment)}{' '}
            enrichment · {number(records.data.counts.accepted)} accepted across the current source,
            import and text filters.
          </p>
        )}
        <p className="help">
          Errors and ignored records explain source import processing. Film and series
          identification is reviewed in{' '}
          <Link className="data-link" to="/metadata/identification">
            Identification
          </Link>
          .
        </p>
      </div>
      <ErrorNotice error={records.error} retry={() => void records.refetch()} />
      {records.isPending ? (
        <Loading />
      ) : (
        records.data && (
          <>
            <ResultCount total={records.data.total} label="processed records" />
            {records.data.items?.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Record / title</TableHead>
                      <TableHead>Format</TableHead>
                      <TableHead>Processing</TableHead>
                      <TableHead>Page</TableHead>
                      <TableHead>Observed</TableHead>
                      <TableHead>
                        <span className="sr-only">View</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {records.data.items.map((record) => (
                      <TableRow key={record.id}>
                        <TableCell className="min-w-52 max-w-xl whitespace-normal">
                          <Link
                            className="font-medium leading-6 hover:text-primary"
                            to={`/raw/${record.id}`}
                            state={listState}
                          >
                            {record.fields?.title != null
                              ? exactField(record.fields.title)
                              : record.source_id || 'Source ID missing'}
                          </Link>
                          <p className="mono mt-1 text-muted-foreground">
                            #{record.id}
                            {record.source_id && ` · ${record.source_id}`}
                          </p>
                          {record.payload_retained === false && (
                            <p className="mt-1 text-xs text-muted-foreground">
                              Raw payload not stored
                            </p>
                          )}
                        </TableCell>
                        <TableCell className="font-mono text-[11px] text-muted-foreground">
                          {record.content_type || 'Not specified'}
                        </TableCell>
                        <TableCell>
                          {record.error ? (
                            <div className="max-w-sm whitespace-normal">
                              <Badge
                                variant="outline"
                                className="border-destructive/30 text-destructive"
                              >
                                Error
                              </Badge>
                              <p className="help mt-1 break-words">{messageText(record.error)}</p>
                            </div>
                          ) : record.auxiliary ? (
                            <Badge variant="outline">Enrichment</Badge>
                          ) : record.ignored ? (
                            <Badge variant="outline">Ignored</Badge>
                          ) : (
                            <span className="text-xs text-muted-foreground">Accepted</span>
                          )}
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {record.page_id > 0 && record.payload_retained !== false ? (
                            <Link
                              className="data-link"
                              to={`/pages/${record.page_id}`}
                              state={listState}
                            >
                              {number(record.page)}
                            </Link>
                          ) : (
                            number(record.page)
                          )}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(record.created_at)}
                        </TableCell>
                        <TableCell>
                          <Button variant="ghost" size="icon-sm" asChild>
                            <Link
                              to={`/raw/${record.id}`}
                              state={listState}
                              aria-label={`View record ${record.id}`}
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
                title="No processed records in this view"
                description="Source records will appear as imports or previews are processed."
                filtered={Boolean(q || processing)}
                offset={offset}
                onFirstPage={() => setSearch(resetListView(search, [], offsetKey))}
              />
            )}
            <Pager
              total={records.data.total}
              limit={records.data.limit}
              offset={records.data.offset}
              busy={records.isFetching}
              onChange={pagination.setOffset}
            />
          </>
        )
      )}
    </div>
  )
}

export function RawDetailPage() {
  const sourceName = useSourceNames()
  const date = useDate()
  const { id = '' } = useParams()
  const detail = useQuery({
    queryKey: ['raw-record', id],
    queryFn: ({ signal }) =>
      api<RawDetail>(`/raw/${encodeURIComponent(id)}`, { signal }, decodeSearchJSON<RawDetail>),
  })
  if (detail.isPending)
    return (
      <div className="page">
        <Loading />
      </div>
    )
  if (!detail.data)
    return (
      <div className="page">
        <PageHeader title="Processed record" back={{ to: '/runs', label: 'Sources history' }} />
        <ErrorNotice error={detail.error} retry={() => void detail.refetch()} />
      </div>
    )
  const { record, raw, byte_length: byteLength } = detail.data
  const payloadRetained = record.payload_retained !== false
  const language = record.content_type.includes('json')
    ? 'json'
    : record.content_type.includes('xml') || raw?.trimStart().startsWith('<')
      ? 'xml'
      : 'text'
  return (
    <div className="page">
      <PageHeader
        title={`Processed record #${record.id}`}
        description={`${sourceName(record.provider_id)} · ${record.source_id || 'Source ID missing'}`}
        back={{
          to: `/runs/${encodeURIComponent(record.run_id)}?tab=raw`,
          label: 'Processed records',
        }}
        actions={
          payloadRetained && (
            <DownloadButton
              path={`/raw/${record.id}/download`}
              filename={`record-${record.id}.${language === 'text' ? 'bin' : language}`}
              label="Download original"
            />
          )
        }
      />
      <ErrorNotice error={detail.error} retry={() => void detail.refetch()} />
      {record.error && <ErrorNotice error={new Error(record.error)} />}
      {!payloadRetained && (
        <p className="notice">
          Original content was not retained. Imported fields, processing results and collection
          checkpoints are preserved.
        </p>
      )}
      {record.auxiliary && (
        <p className="notice">
          Metadata update record. It preserves processing history and does not represent an
          additional torrent.
        </p>
      )}
      {record.ignored && (
        <p className="notice">
          Record ignored for publication; its processing result is preserved.
        </p>
      )}
      <dl className="grid gap-5 border-y border-border py-5 text-sm sm:grid-cols-2 xl:grid-cols-4">
        <div>
          <dt className="text-xs text-muted-foreground">Received format</dt>
          <dd className="mono mt-2 break-all">{record.content_type || 'Not specified'}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Original size</dt>
          <dd className="mt-2">
            {payloadRetained && byteLength !== null ? `${number(byteLength)} bytes` : 'Not stored'}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Observed</dt>
          <dd className="mt-2">{date(record.created_at)}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted-foreground">Source page</dt>
          <dd className="mt-2">
            {record.page_id > 0 && payloadRetained ? (
              <Link
                className="data-link inline-flex items-center gap-2"
                to={`/pages/${record.page_id}`}
              >
                <FileTextIcon />
                Page {record.page} · #{record.page_id}
              </Link>
            ) : record.page_id > 0 ? (
              `Page ${record.page} · response not stored`
            ) : (
              'Unavailable'
            )}
          </dd>
        </div>
      </dl>
      <Tabs defaultValue={payloadRetained ? 'original' : 'fields'}>
        <TabsList>
          <TabsTrigger value="original">Original content</TabsTrigger>
          <TabsTrigger value="fields">Interpreted fields</TabsTrigger>
        </TabsList>
        <TabsContent value="original" className="mt-5 space-y-3">
          {payloadRetained ? (
            <>
              <p className="help">
                Text view without reformatting. The download preserves the original bytes, including
                non-text data.
              </p>
              <CodeEditor
                value={raw ?? ''}
                language={language}
                label="Original record content"
                height="600px"
                readOnly
              />
            </>
          ) : (
            <p className="notice">Original content was not stored for this record.</p>
          )}
        </TabsContent>
        <TabsContent value="fields" className="mt-5 space-y-3">
          {record.fields == null || Object.keys(record.fields).length === 0 ? (
            <p className="notice">
              No parsed fields are stored with this record. Full, Incremental and Metadata keep
              fields in the catalog or private Full staging, not duplicate snapshots here. Preview
              keeps its parsed sample here without publication.
            </p>
          ) : (
            <>
              <p className="help">
                Parsed fields are separate from raw payloads. Missing values are not replaced by
                zero.
              </p>
              <CodeEditor
                value={exactJSON(record.fields)}
                language="json"
                label="Interpreted record fields"
                height="460px"
                readOnly
              />
            </>
          )}
        </TabsContent>
      </Tabs>
      {record.page_id > 0 && payloadRetained && (
        <section className="flex flex-wrap items-center justify-between gap-4 border-t border-border pt-5">
          <div>
            <h2 className="section-title">Complete provider response</h2>
            <p className="help mt-1">The page contains this record’s original context.</p>
          </div>
          <div className="toolbar">
            <Button variant="ghost" asChild>
              <Link to={`/pages/${record.page_id}`}>
                View page
                <ArrowRightIcon />
              </Link>
            </Button>
            <DownloadButton
              path={`/pages/${record.page_id}/download`}
              filename={`page-${record.page_id}.bin`}
              label="Download page"
            />
          </div>
        </section>
      )}
    </div>
  )
}

export function RawPageView() {
  const { id = '' } = useParams()
  const page = useQuery({
    queryKey: ['raw-page', id],
    queryFn: ({ signal }) => rawPage(id, signal),
    staleTime: Infinity,
    // Immutable archive bytes need one download; failed reads still recover.
    refetchInterval: (query) =>
      query.state.status === 'success' ? false : AUTO_REFRESH_INTERVAL(),
  })
  const text = page.data?.text ?? ''
  const trimmed = text.trimStart()
  const language =
    page.data?.contentType.includes('json') || trimmed.startsWith('{') || trimmed.startsWith('[')
      ? 'json'
      : page.data?.contentType.includes('xml') || trimmed.startsWith('<')
        ? 'xml'
        : 'text'
  return (
    <div className="page">
      <PageHeader
        title={`Original page #${id}`}
        description="Historical response archive. New response bodies are not stored."
        back={{ to: '/runs', label: 'Sources history' }}
        actions={
          page.data && (
            <DownloadButton
              path={`/pages/${encodeURIComponent(id)}/download`}
              filename={`page-${id}.bin`}
              label="Download page"
            />
          )
        }
      />
      {page.error instanceof ApiError && page.error.status === 404 ? (
        <p className="notice">
          Response unavailable. New response bodies are not stored; historical archived responses
          remain available when their page exists.
        </p>
      ) : (
        <ErrorNotice error={page.error} retry={() => void page.refetch()} />
      )}
      {page.isPending ? (
        <Loading />
      ) : (
        page.data && (
          <>
            <div className="notice flex flex-wrap items-center justify-between gap-3">
              <span>{number(page.data.byteLength)} original bytes</span>
              <span className="text-xs text-muted-foreground">
                UTF-8 display · no HTML executed · exact download
              </span>
            </div>
            <CodeEditor
              value={page.data.text}
              language={language}
              label="Complete original provider response"
              readOnly
              height="650px"
            />
          </>
        )
      )}
    </div>
  )
}
