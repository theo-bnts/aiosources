import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ArrowRightIcon } from '@phosphor-icons/react'
import { api, params } from '@/lib/api'
import { useSourceNames } from '@/lib/source-names'
import type { CatalogOrigin, Page, Torrent } from '@/lib/types'
import type { OccurrenceHistory, RunChanges, TorrentChange } from '@/lib/history-types'
import { occurrenceHref } from '@/lib/search-types'
import { decodeSearchJSON, exactField, exactJSON, fieldChanges } from '@/lib/search-json'
import { bytes, useDate, number } from '@/lib/format'
import { useURLPagination } from '@/lib/pagination'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CodeEditor } from '@/components/code-editor'
import { ListEmpty, ErrorNotice, Loading, Pager } from '@/components/common'

export function TorrentTable({
  items,
  onOpen,
}: {
  items: Torrent[]
  onOpen?: (torrent: Torrent, target: HTMLElement) => void
}) {
  const date = useDate()
  const sourceName = useSourceNames()
  return (
    <div className="table-frame">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Title / source ID</TableHead>
            <TableHead>Source</TableHead>
            <TableHead className="text-right">Size</TableHead>
            <TableHead>Last seen</TableHead>
            <TableHead>
              <span className="sr-only">Open processed record</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((torrent) => (
            <TableRow key={torrent.occurrence_id}>
              <TableCell className="min-w-64 max-w-lg whitespace-normal">
                <Link
                  className="data-link text-left font-medium leading-6"
                  to={occurrenceHref(torrent.occurrence_id)}
                  onClick={(event) => {
                    if (
                      onOpen &&
                      !event.ctrlKey &&
                      !event.metaKey &&
                      !event.shiftKey &&
                      !event.altKey &&
                      event.button === 0
                    ) {
                      event.preventDefault()
                      onOpen(torrent, event.currentTarget)
                    }
                  }}
                >
                  {torrent.fields?.title != null ? exactField(torrent.fields.title) : 'Untitled'}
                </Link>
                <p className="mono mt-1 break-all text-muted-foreground">{torrent.source_id}</p>
              </TableCell>
              <TableCell className="text-xs">
                {sourceName(torrent.provider_id)}
                {torrent.origin && <p className="mt-1 text-muted-foreground">Remote · read-only</p>}
              </TableCell>
              <TableCell
                className="text-right font-mono text-xs"
                title={`${exactField(torrent.fields?.size)} bytes`}
              >
                {bytes(torrent.fields?.size)}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {date(torrent.last_seen_at)}
              </TableCell>
              <TableCell>
                {torrent.raw_id != null ? (
                  <Button asChild variant="ghost" size="icon-sm">
                    <Link
                      to={`/raw/${torrent.raw_id}`}
                      aria-label={`Open processed record for ${torrent.source_id}`}
                    >
                      <ArrowRightIcon />
                    </Link>
                  </Button>
                ) : (
                  <span className="text-muted-foreground" title="Processed record unavailable">
                    —
                  </span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function Provenance({
  origin,
  title = 'Original provenance',
}: {
  origin?: CatalogOrigin
  title?: string
}) {
  if (!origin) return null
  return (
    <section className="notice space-y-3">
      <h3 className="font-medium">{title}</h3>
      <p className="help">
        Read-only imported values. The original identity is preserved independently of every other
        source occurrence.
      </p>
      <dl className="grid gap-3 text-xs sm:grid-cols-3">
        {(
          [
            ['Origin instance', origin.instance_id],
            ['Origin provider', origin.provider_id],
            ['Origin record', origin.source_id],
          ] as const
        ).map(([label, value]) => (
          <div className="min-w-0" key={label}>
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="mono mt-1 break-all">{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function ChangeFieldDiff({ change }: { change: TorrentChange }) {
  const differences = fieldChanges(change.before, change.after)
  const provenanceChanged =
    change.before_origin?.instance_id !== change.origin?.instance_id ||
    change.before_origin?.provider_id !== change.origin?.provider_id ||
    change.before_origin?.source_id !== change.origin?.source_id
  return (
    <div className="space-y-4">
      {differences.length ? (
        <div className="divide-y divide-border rounded-md border border-border">
          {differences.map((difference) => (
            <div className="space-y-3 p-3" key={difference.key}>
              <div className="flex flex-wrap items-center gap-2">
                <code className="break-all text-xs">{difference.key}</code>
                <Badge variant="outline">{difference.kind}</Badge>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <div className="min-w-0">
                  <p className="mb-1 text-xs text-muted-foreground">Before</p>
                  {difference.had ? (
                    <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/40 p-3 font-mono text-xs">
                      {exactJSON(difference.before)}
                    </pre>
                  ) : (
                    <p className="rounded border border-dashed border-border p-3 text-xs text-muted-foreground">
                      Field absent
                    </p>
                  )}
                </div>
                <div className="min-w-0">
                  <p className="mb-1 text-xs text-muted-foreground">After</p>
                  {difference.has ? (
                    <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/40 p-3 font-mono text-xs">
                      {exactJSON(difference.after)}
                    </pre>
                  ) : (
                    <p className="rounded border border-dashed border-border p-3 text-xs text-muted-foreground">
                      Field absent
                    </p>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <p className="help">
          No mapped field differences. This transition may change the occurrence’s publication state
          or provenance.
        </p>
      )}
      <Disclosure title="Complete before / after snapshots" variant="inline">
        <div className="grid gap-3 md:grid-cols-2">
          {(['before', 'after'] as const).map((key) => (
            <div className="min-w-0" key={key}>
              <p className="mb-1 text-xs text-muted-foreground">
                {key === 'before' ? 'Before' : 'After'}
              </p>
              {change[key] === null ? (
                <p className="help">Occurrence not published in this state.</p>
              ) : (
                <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/40 p-3 font-mono text-xs">
                  {exactJSON(change[key])}
                </pre>
              )}
            </div>
          ))}
        </div>
      </Disclosure>
      {provenanceChanged ? (
        <div className="space-y-3">
          {change.before_origin ? (
            <Provenance origin={change.before_origin} title="Provenance before publication" />
          ) : (
            <p className="notice">
              {change.before === null
                ? 'Before: no published occurrence.'
                : 'Before: local source provenance.'}
            </p>
          )}
          {change.origin ? (
            <Provenance origin={change.origin} title="Provenance after publication" />
          ) : (
            <p className="notice">
              {change.after === null
                ? 'After: no published occurrence.'
                : 'After: local source provenance.'}
            </p>
          )}
        </div>
      ) : (
        <Provenance origin={change.origin} />
      )}
    </div>
  )
}

function ChangeEntry({ change }: { change: TorrentChange }) {
  const date = useDate()
  return (
    <Disclosure
      title={
        <>
          <span className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <Badge
              variant="outline"
              className={change.kind === 'deleted' ? 'border-destructive/30 text-destructive' : ''}
            >
              {change.kind === 'added'
                ? 'Added'
                : change.kind === 'deleted'
                  ? 'Deleted'
                  : 'Updated'}
            </Badge>
            <span>{date(change.occurred_at)}</span>
            <span className="text-xs text-muted-foreground">View field differences</span>
          </span>
        </>
      }
    >
      {change.run_id ? (
        <Link
          className="data-link inline-flex items-center gap-2 break-all text-xs"
          to={`/runs/${encodeURIComponent(change.run_id)}?tab=changes`}
        >
          Run {change.run_id}
          <ArrowRightIcon className="shrink-0" />
        </Link>
      ) : (
        <p className="help">No run attribution recorded for this publication.</p>
      )}
      <ChangeFieldDiff change={change} />
    </Disclosure>
  )
}

export function OccurrenceDialog({
  id,
  torrent,
  onClose,
  onOpen,
  onReturnFocus,
}: {
  id: string
  torrent?: Torrent
  onClose: () => void
  onOpen: (torrent: Torrent, target: HTMLElement) => void
  onReturnFocus: () => void
}) {
  const date = useDate()
  const sourceName = useSourceNames()
  const historyPage = useURLPagination({ limitKey: 'history_limit', offsetKey: 'history_offset' })
  const relatedPage = useURLPagination({ limitKey: 'related_limit', offsetKey: 'related_offset' })
  const [search, setSearch] = useViewSearchParams()
  const valid = /^[a-f0-9]{64}$/.test(id)
  const historyOffset = historyPage.offset
  const relatedOffset = relatedPage.offset
  const requestedTab = search.get('occurrence_tab')
  const activeTab =
    requestedTab === 'related' ||
    requestedTab === 'history' ||
    (requestedTab === 'fields' && torrent)
      ? requestedTab
      : torrent
        ? 'fields'
        : 'history'
  const history = useQuery({
    queryKey: ['torrent-history', id, historyOffset, historyPage.limit],
    queryFn: ({ signal }) =>
      api<OccurrenceHistory>(
        `/torrents/${encodeURIComponent(id)}/history?${params({ limit: historyPage.limit, offset: historyOffset })}`,
        { signal },
        decodeSearchJSON<OccurrenceHistory>,
      ),
    enabled: valid,
  })
  const related = useQuery({
    queryKey: ['torrent-related', id, relatedOffset, relatedPage.limit],
    queryFn: ({ signal }) =>
      api<Page<Torrent>>(
        `/torrents/${encodeURIComponent(id)}/related?${params({ limit: relatedPage.limit, offset: relatedOffset })}`,
        { signal },
        decodeSearchJSON<Page<Torrent>>,
      ),
    enabled: valid && activeTab === 'related',
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent
        className="min-w-0 sm:max-w-5xl"
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          onReturnFocus()
        }}
      >
        <DialogHeader>
          <DialogTitle>Source occurrence</DialogTitle>
          <DialogDescription className="break-all">
            {torrent || history.data
              ? `${sourceName(torrent?.provider_id ?? history.data?.provider_id)} · ${torrent?.source_id ?? history.data?.source_id}`
              : 'Publication history and related source records.'}
          </DialogDescription>
        </DialogHeader>
        {!valid ? (
          <ErrorNotice
            error={
              new Error(
                'This occurrence ID is invalid. Open a record from the torrent list or run changes.',
              )
            }
          />
        ) : (
          <>
            <p className="help">
              Every source occurrence is independent. Matching info hashes do not merge records or
              remove them from search.
            </p>
            <Tabs
              value={activeTab}
              onValueChange={(value) => {
                const next = new URLSearchParams(search)
                next.set('occurrence_tab', value)
                setSearch(next, { replace: true })
              }}
            >
              <div className="overflow-x-auto">
                <TabsList className="flex-wrap group-data-[orientation=horizontal]/tabs:h-auto">
                  {torrent && <TabsTrigger value="fields">Published fields</TabsTrigger>}
                  <TabsTrigger value="history">Change history</TabsTrigger>
                  <TabsTrigger value="related">Related occurrences</TabsTrigger>
                </TabsList>
              </div>
              {torrent && (
                <TabsContent value="fields" className="mt-5 space-y-4">
                  <dl className="grid grid-cols-2 gap-4 text-xs">
                    <div>
                      <dt className="text-muted-foreground">First seen</dt>
                      <dd className="mt-1">{date(torrent.first_seen_at)}</dd>
                    </div>
                    <div>
                      <dt className="text-muted-foreground">Last seen</dt>
                      <dd className="mt-1">{date(torrent.last_seen_at)}</dd>
                    </div>
                  </dl>
                  <Provenance origin={torrent.origin} />
                  {torrent.historical && (
                    <p className="notice">
                      Imported normalized history. No raw original was fabricated for this data.
                    </p>
                  )}
                  <CodeEditor
                    label="Interpreted torrent fields"
                    value={exactJSON(torrent.fields ?? {})}
                    language="json"
                    readOnly
                    height="400px"
                  />
                  {torrent.raw_id != null && (
                    <Button asChild>
                      <Link to={`/raw/${torrent.raw_id}`}>
                        View processed record
                        <ArrowRightIcon />
                      </Link>
                    </Button>
                  )}
                </TabsContent>
              )}
              <TabsContent value="history" className="mt-5 space-y-4">
                <ErrorNotice error={history.error} retry={() => void history.refetch()} />
                {history.isPending ? (
                  <Loading label="Loading occurrence history" />
                ) : (
                  history.data && (
                    <>
                      <p className="help">
                        Semantic publication changes, newest first. Processed records without
                        changed values are not updates. Missing fields and explicit null values
                        remain distinct.
                      </p>
                      <Provenance origin={history.data.origin} />
                      {history.data.history_since && (
                        <p className="help">
                          Detailed history tracking began {date(history.data.history_since)}.
                        </p>
                      )}
                      {history.data.baseline && (
                        <Disclosure
                          title={
                            <>
                              Pre-existing{' '}
                              {history.data.baseline.deleted ? 'retired occurrence' : 'snapshot'} ·
                              baseline
                            </>
                          }
                          className="bg-muted/50"
                        >
                          <div className="space-y-3">
                            <p className="help">
                              Baseline recorded {date(history.data.baseline.recorded_at)} when
                              history tracking began. This is not an addition attributed to an
                              earlier run; older field transitions may not be available.
                            </p>
                            <Provenance origin={history.data.baseline.origin} />
                            {history.data.baseline.fields === null ? (
                              <p className="help">
                                No previous mapped field snapshot is available.
                              </p>
                            ) : (
                              <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all font-mono text-xs">
                                {exactJSON(history.data.baseline.fields)}
                              </pre>
                            )}
                          </div>
                        </Disclosure>
                      )}
                      {history.data.items?.length ? (
                        history.data.items.map((change) => (
                          <ChangeEntry key={change.id} change={change} />
                        ))
                      ) : (
                        <ListEmpty
                          title={
                            historyOffset
                              ? 'No changes on this page'
                              : 'No tracked publication changes'
                          }
                          description={
                            history.data.baseline
                              ? 'The retained baseline is available above. No semantic transitions are recorded in this view.'
                              : 'No semantic transitions are recorded in this view. Retained legacy identities may not have earlier field snapshots.'
                          }
                          offset={historyOffset}
                          onFirstPage={() => historyPage.setOffset(0)}
                        />
                      )}
                      <Pager
                        total={history.data.total}
                        limit={history.data.limit}
                        offset={history.data.offset}
                        busy={history.isFetching}
                        onChange={historyPage.setOffset}
                      />
                    </>
                  )
                )}
              </TabsContent>
              <TabsContent value="related" className="mt-5 space-y-4">
                <p className="help">
                  Other live occurrences with the same nonempty info hash. Each source record is
                  listed separately, including multiple records from one source.
                </p>
                <ErrorNotice error={related.error} retry={() => void related.refetch()} />
                {related.isPending ? (
                  <Loading label="Loading related occurrences" />
                ) : (
                  related.data && (
                    <>
                      {related.data.items?.length ? (
                        <TorrentTable items={related.data.items} onOpen={onOpen} />
                      ) : (
                        <ListEmpty
                          title="No related occurrences"
                          description="There are no other live records with this info hash in the current page. Records without a usable info hash have no related matches."
                          offset={relatedOffset}
                          onFirstPage={() => relatedPage.setOffset(0)}
                        />
                      )}
                      <Pager
                        total={related.data.total}
                        limit={related.data.limit}
                        offset={related.data.offset}
                        busy={related.isFetching}
                        onChange={relatedPage.setOffset}
                      />
                    </>
                  )
                )}
              </TabsContent>
            </Tabs>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function RunPublicationCounts({ data }: { data: RunChanges }) {
  const date = useDate()
  return (
    <section className="space-y-3" aria-labelledby="publication-counts-title">
      <h2 id="publication-counts-title" className="section-title">
        Published changes
      </h2>
      {!data.history_complete && (
        <p className="notice">
          This run predates complete history tracking
          {data.history_since ? ` (${date(data.history_since)})` : ''}. The values below show only
          recorded changes, not a reconstructed total for the run.
        </p>
      )}
      <dl className="grid grid-cols-3 divide-x divide-border rounded-md border border-border py-4 text-center">
        {(['added', 'updated', 'deleted'] as const).map((kind) => (
          <div key={kind} className="min-w-0 px-2 sm:px-4">
            <dt className="text-xs text-muted-foreground">
              {kind === 'added' ? 'Added' : kind === 'updated' ? 'Updated' : 'Deleted'}
              {!data.history_complete ? ' (recorded)' : ''}
            </dt>
            <dd className="mt-2 font-mono text-xl tabular-nums sm:text-2xl">
              {number(data.counts[kind])}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

export function RunChangesList({
  data,
  busy,
  onPage,
}: {
  data: RunChanges
  busy: boolean
  onPage: (offset: number) => void
}) {
  const sourceName = useSourceNames()
  return (
    <div className="space-y-4">
      {data.items?.length ? (
        data.items.map((change) => (
          <section className="space-y-3" key={change.id}>
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <h3 className="break-words text-sm font-medium">
                  {(change.after ?? change.before)?.title == null
                    ? 'Untitled occurrence'
                    : exactField((change.after ?? change.before)?.title)}
                </h3>
                <p className="mono mt-1 break-all text-muted-foreground">
                  {sourceName(change.provider_id)} · {change.source_id}
                </p>
              </div>
              <Button variant="outline" size="sm" asChild>
                <Link to={occurrenceHref(change.occurrence_id)}>
                  Occurrence history
                  <ArrowRightIcon />
                </Link>
              </Button>
            </div>
            <ChangeEntry change={change} />
          </section>
        ))
      ) : (
        <ListEmpty
          title={
            data.offset
              ? 'No changes on this page'
              : data.history_complete
                ? 'No published changes'
                : 'No tracked changes for this run'
          }
          description={
            data.history_complete
              ? 'This run has not committed any additions, field updates, or deletions. Processed records are shown separately.'
              : 'Earlier publications were not fully tracked. No change totals have been inferred from processed record counts.'
          }
          offset={data.offset}
          onFirstPage={() => onPage(0)}
        />
      )}
      <Pager
        total={data.total}
        limit={data.limit}
        offset={data.offset}
        busy={busy}
        onChange={onPage}
      />
    </div>
  )
}
