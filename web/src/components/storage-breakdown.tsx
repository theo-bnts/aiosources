import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowRightIcon, CaretRightIcon } from '@phosphor-icons/react'
import { bytes, number, percentage, useDate } from '@/lib/format'
import { useSourceNames } from '@/lib/source-names'
import type { SystemHealth, StorageCategory } from '@/lib/health-types'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Disclosure } from '@/components/ui/disclosure'
import { Pager, RunStatusBadge } from '@/components/common'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const labels: Record<string, string> = {
  sources: 'Sources',
  metadata: 'Metadata',
  debrid: 'Debrid',
  stremio: 'Stremio',
  sharing: 'Catalog sharing',
  access: 'Security',
  logs: 'Logs and operation history',
  other: 'Settings and database state',
  overhead: 'Database overhead',
  published: 'Published torrents',
  imports: 'Prepared imports and resume state',
  'source-history': 'Source history',
  information: 'Film and series information',
  identification: 'Identification and reviews',
  'information-queue': 'Information inventory and pending updates',
  availability: 'Availability cache',
  'debrid-operations': 'Temporary cleanup and service state',
  'shared-snapshots': 'Shared catalog snapshots and changes',
  'stream-history': 'Stream request history',
  'activity-history': 'Activity logs',
  'operation-history': 'Operation history',
  state: 'Settings and working state',
}

const explanations: Record<string, string> = {
  published:
    'Current source records used by Search, identification and playback, including imported remote catalogs.',
  imports:
    'Unpublished data and checkpoints used to complete or resume imports. Successful publication releases that run’s preparation data.',
  'source-history':
    'Collection diagnostics and published changes. Diagnostic content expires after seven days; evidence needed to resume unfinished work can remain.',
  information: 'Local titles, identifiers and season information used for identification.',
  identification: 'Current identification decisions, manual reviews and pending work.',
  'information-queue': 'The information inventory and durable work waiting for an update.',
  availability:
    'Reusable availability observations and account-specific checks. Expired entries are cleaned automatically.',
  'debrid-operations':
    'Temporary resource ownership and cleanup state, retained until handled safely.',
  'shared-snapshots':
    'Snapshots and changes needed by enabled catalog shares. Imported remote records are counted under Sources.',
  'stream-history':
    'Request summaries and returned results, retained for seven days. Service keys and playback credentials are excluded.',
  'activity-history': 'Application activity and sanitized error messages, retained for seven days.',
  'operation-history':
    'Information updates, identification runs and availability check history, retained for seven days.',
  state: 'Configuration, permissions and checkpoints needed by this feature.',
}

function AllocationDetails({ category }: { category: StorageCategory }) {
  return (
    <div className="space-y-4">
      <dl className="flex flex-wrap gap-x-6 gap-y-2 text-xs">
        <div>
          <dt className="inline text-muted-foreground">Data: </dt>
          <dd className="inline tabular-nums">{bytes(category.table_bytes)}</dd>
        </div>
        <div>
          <dt className="inline text-muted-foreground">Indexes: </dt>
          <dd className="inline tabular-nums">{bytes(category.index_bytes)}</dd>
        </div>
      </dl>
      {category.details?.length === 1 && (
        <p className="help">{explanations[category.details[0].key]}</p>
      )}
      {category.details && category.details.length > 1 && (
        <div className="table-frame">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Purpose</TableHead>
                <TableHead className="text-right">Data</TableHead>
                <TableHead className="text-right">Indexes</TableHead>
                <TableHead className="text-right">Total</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {category.details.map((part) => (
                <TableRow key={part.key}>
                  <TableCell className="min-w-52 whitespace-normal">
                    <p>{labels[part.key] || part.key}</p>
                    <p className="help mt-1 max-w-xl">{explanations[part.key]}</p>
                  </TableCell>
                  <TableCell className="text-right mono">{bytes(part.table_bytes)}</TableCell>
                  <TableCell className="text-right mono">{bytes(part.index_bytes)}</TableCell>
                  <TableCell className="text-right mono">{bytes(part.bytes)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

function SourceStorageDetails({ report }: { report: SystemHealth }) {
  const sourceName = useSourceNames()
  const date = useDate()
  const [sourceOffset, setSourceOffset] = useState(0)
  const sources = [...(report.source_storage ?? [])].sort(
    (a, b) => b.bytes - a.bytes || a.provider_id.localeCompare(b.provider_id),
  )
  const offset = Math.min(sourceOffset, Math.max(0, Math.ceil(sources.length / 50) - 1) * 50)
  return (
    <div className="space-y-4">
      <Disclosure title="By source (estimated)">
        {sources.length ? (
          <div className="table-frame">
            <Table className="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/2">Source</TableHead>
                  <TableHead className="w-1/2 text-right whitespace-normal">
                    Estimated allocation
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sources.slice(offset, offset + 50).map((source) => (
                  <TableRow key={source.provider_id}>
                    <TableCell className="break-words whitespace-normal">
                      {sourceName(source.provider_id)}
                    </TableCell>
                    <TableCell className="text-right mono">{bytes(source.bytes)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : (
          <p className="help">
            Source estimates appear when PostgreSQL has analyzed the stored data.
          </p>
        )}
        {sources.length > 50 && (
          <Pager total={sources.length} limit={50} offset={offset} onChange={setSourceOffset} />
        )}
        <p className="help">
          Includes source records, preparation data, retained history and indexes. Estimates use
          PostgreSQL statistics and can lag recent imports. Shared metadata and caches are separate.
        </p>
      </Disclosure>
      <Disclosure
        title={
          <span className="flex flex-wrap items-center gap-2">
            Imports with prepared data{' '}
            {report.import_storage_available && (
              <Badge variant={report.cancelled_import_count ? 'warning' : 'secondary'}>
                {number(report.retained_import_count ?? 0)}
              </Badge>
            )}
          </span>
        }
      >
        {!report.import_storage_available ? (
          <p className="help">Import storage estimates are currently unavailable.</p>
        ) : (report.retained_import_count ?? 0) > 0 ? (
          <>
            {(report.cancelled_import_count ?? 0) > 0 && (
              <p className="help text-amber-300">
                {number(report.cancelled_import_count ?? 0)} cancelled import
                {report.cancelled_import_count === 1 ? ' keeps' : 's keep'} prepared data for
                resuming. This data does not expire after seven days.
              </p>
            )}
            <div className="table-frame">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Source / status</TableHead>
                    <TableHead className="text-right whitespace-normal">Estimated size</TableHead>
                    <TableHead className="w-12">
                      <span className="sr-only">Open</span>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {report.retained_imports?.map((run) => (
                    <TableRow key={run.run_id}>
                      <TableCell className="whitespace-normal">
                        <p className="break-words">
                          {sourceName(run.provider_id, run.provider_name)}
                        </p>
                        <p className="help mt-1">{date(run.created_at)}</p>
                        <div className="mt-2">
                          <RunStatusBadge
                            run={{
                              status: run.status,
                              cancel_requested: false,
                              pause_requested: false,
                            }}
                          />
                        </div>
                      </TableCell>
                      <TableCell className="text-right mono">
                        {run.bytes === undefined ? 'Not yet estimated' : bytes(run.bytes)}
                      </TableCell>
                      <TableCell>
                        <Button asChild variant="ghost" size="icon-sm">
                          <Link
                            to={`/runs/${encodeURIComponent(run.run_id)}`}
                            aria-label={`Open import from ${sourceName(run.provider_id, run.provider_name)} on ${date(run.created_at)}`}
                          >
                            <ArrowRightIcon aria-hidden="true" />
                          </Link>
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            {(report.retained_import_count ?? 0) > (report.retained_imports?.length ?? 0) && (
              <p className="help">
                The 20 largest estimates are shown. All imports remain available in Sources →
                History.
              </p>
            )}
          </>
        ) : (
          <p className="help">No imports currently retain prepared data.</p>
        )}
        <p className="help">
          Includes unpublished records and resume state. History and published records are counted
          separately. Resuming continues the same data; starting another Full import can create a
          separate preparation set.
        </p>
      </Disclosure>
    </div>
  )
}

export function StorageBreakdown({ report }: { report: SystemHealth }) {
  const [expanded, setExpanded] = useState<string | null>(() =>
    location.hash === '#source-storage' ? 'sources' : null,
  )
  const items: StorageCategory[] = [
    ...report.database.categories,
    {
      key: 'overhead',
      bytes: report.database.other_database_bytes,
      table_bytes: 0,
      index_bytes: 0,
    },
  ].sort((a, b) => b.bytes - a.bytes || a.key.localeCompare(b.key))
  return (
    <section className="section-card" id="source-storage" aria-labelledby="storage-usage-title">
      <h2 className="section-title" id="storage-usage-title">
        Where storage is used
      </h2>
      {report.database.measured_at ? (
        <>
          <div className="table-frame">
            <Table className="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/2 whitespace-normal">Stored data</TableHead>
                  <TableHead className="w-1/4 text-right whitespace-normal">
                    Space allocated
                  </TableHead>
                  <TableHead className="w-1/4 text-right" title="Share of database">
                    Share
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((category) => (
                  <Fragment key={category.key}>
                    <TableRow>
                      <TableCell className="whitespace-normal">
                        {category.key === 'overhead' ? (
                          <span className="inline-flex min-h-8 items-center">
                            {labels.overhead}
                          </span>
                        ) : (
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            className="-ml-2 h-auto min-h-8 max-w-full justify-start whitespace-normal text-left"
                            aria-expanded={expanded === category.key}
                            aria-controls={`storage-${category.key}`}
                            onClick={() =>
                              setExpanded(expanded === category.key ? null : category.key)
                            }
                          >
                            <CaretRightIcon
                              aria-hidden="true"
                              className={expanded === category.key ? 'rotate-90' : ''}
                            />
                            <span className="min-w-0 break-words">
                              {labels[category.key] || category.key}
                            </span>
                          </Button>
                        )}
                      </TableCell>
                      <TableCell className="text-right mono">{bytes(category.bytes)}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">
                        {category.bytes >= 0
                          ? percentage(category.bytes, report.database.total_bytes)
                          : 'Not available'}
                      </TableCell>
                    </TableRow>
                    {expanded === category.key && category.key !== 'overhead' && (
                      <TableRow id={`storage-${category.key}`}>
                        <TableCell colSpan={3} className="min-w-0 whitespace-normal p-4 sm:p-5">
                          <div className="min-w-0 space-y-4 overflow-hidden">
                            <AllocationDetails category={category} />
                            {category.key === 'sources' && <SourceStorageDetails report={report} />}
                          </div>
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          </div>
          <p className="help">
            Measured PostgreSQL allocation includes tables, indexes and reusable space. It is
            separate from torrent file sizes. Expand a category for details.
          </p>
          <Disclosure title="Retention and estimates" variant="inline">
            <p className="help">
              Diagnostic history older than seven days is removed automatically in batches.
              Published torrents, metadata and current identification decisions remain available.
            </p>
            <p className="help">
              Unfinished Full imports, including failed and cancelled runs, keep preparation data
              needed to resume. Their checkpoints and coverage evidence can remain beyond seven
              days. Successful publication clears preparation data for that run.
            </p>
            <p className="help">
              Deleting records makes PostgreSQL space reusable; it does not necessarily shrink
              database files. Per-source and per-import sizes are estimates from PostgreSQL
              statistics, not a scan of all records. Independent measurements can briefly differ
              during writes.
            </p>
          </Disclosure>
        </>
      ) : (
        <p className="help">
          Storage usage will appear after the first successful database measurement.
        </p>
      )}
    </section>
  )
}
