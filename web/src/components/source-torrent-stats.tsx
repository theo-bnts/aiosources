import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, params, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { bytes, number, percentage, useDate } from '@/lib/format'
import { identificationResults, identificationStatuses } from '@/lib/identification'
import type { SourceTorrentOverview, SourceTorrentStats as TorrentStats } from '@/lib/types'

export function useSourceTorrentStats(enabled: boolean) {
  const query = useQuery({
    queryKey: ['source-torrent-stats'],
    queryFn: ({ signal }) => api<SourceTorrentOverview>('/source-torrent-stats', { signal }),
    enabled,
    refetchInterval: enabled ? COLLECTION_REFRESH_INTERVAL : false,
  })
  const bySource = useMemo(
    () => new Map(query.data?.items?.map((item) => [item.provider_id, item])),
    [query.data],
  )
  return { ...query, bySource, totals: query.data?.totals }
}

export function SourceTorrentStats({
  source,
  counts,
  unavailable,
  totals,
}: {
  source: string
  counts?: TorrentStats
  unavailable: boolean
  totals?: SourceTorrentOverview['totals']
}) {
  const date = useDate()
  if (!counts)
    return (
      <p className={`text-sm ${unavailable ? 'text-destructive' : 'text-muted-foreground'}`}>
        {unavailable ? 'Counts unavailable' : 'Loading counts…'}
      </p>
    )
  return (
    <div className="min-w-52 space-y-3">
      <dl className="grid grid-cols-2 gap-x-5 gap-y-2 text-sm">
        <div className="col-span-2">
          <dt className="text-muted-foreground">Imported torrents</dt>
          <dd className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1">
            <Link
              className="data-link font-medium tabular-nums"
              to={`/torrents?${params({ provider: counts.provider_id })}`}
              aria-label={`View ${number(counts.imported)} imported torrents from ${source}`}
            >
              {number(counts.imported)}
              {totals && (
                <span className="font-normal">
                  {' '}
                  ({percentage(counts.imported, totals.imported)})
                </span>
              )}
            </Link>
            {counts.storage && (
              <span
                className="text-xs text-muted-foreground"
                title={`Estimated PostgreSQL source data and indexes, including retained import history. Measured ${date(counts.storage.measured_at)}. Shared metadata and caches are separate.`}
              >
                · {bytes(counts.storage.bytes)} estimated
              </span>
            )}
            {!counts.storage && (
              <span className="text-xs text-muted-foreground">· Size not yet estimated</span>
            )}
          </dd>
        </div>
        {identificationStatuses.map((status) => (
          <div key={status}>
            <dt className="text-xs text-muted-foreground">{identificationResults[status].label}</dt>
            <dd className="mt-1">
              <Link
                className="data-link tabular-nums"
                to={`/metadata/identification?${params({ match_status: status, match_provider: counts.provider_id })}`}
                aria-label={`View ${number(counts[status])} ${identificationResults[status].label.toLowerCase()} torrents from ${source}`}
              >
                {number(counts[status])}
                <span> ({percentage(counts[status], counts.imported)})</span>
              </Link>
            </dd>
          </div>
        ))}
      </dl>
      {unavailable && (
        <p role="status" className="help text-amber-300">
          Last available counts.
        </p>
      )}
      {counts.pending_import && (
        <div className="space-y-2 border-t pt-2 text-xs text-muted-foreground">
          <Link
            className="data-link tabular-nums"
            to={`/runs/${encodeURIComponent(counts.pending_import.run_id)}`}
          >
            {number(counts.pending_import.torrents)} awaiting import completion
            {totals && (
              <span>
                {' '}
                ({percentage(counts.pending_import.torrents, totals.awaiting_import)} of all
                awaiting)
              </span>
            )}
          </Link>
          <dl className="flex flex-wrap gap-x-4 gap-y-1">
            <div>
              <dt className="inline">New records: </dt>
              <dd className="inline tabular-nums">
                {number(counts.pending_import.new)} (
                {percentage(counts.pending_import.new, counts.pending_import.torrents)})
              </dd>
            </div>
            <div>
              <dt className="inline">Updates: </dt>
              <dd className="inline tabular-nums">
                {number(counts.pending_import.updates)} (
                {percentage(counts.pending_import.updates, counts.pending_import.torrents)})
              </dd>
            </div>
          </dl>
        </div>
      )}
    </div>
  )
}
