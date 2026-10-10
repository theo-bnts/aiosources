import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, params } from '@/lib/api'
import { number } from '@/lib/format'
import type { Items, SourceTorrentStats as TorrentStats } from '@/lib/types'

const decisions = [
  { status: 'matched', label: 'Identified' },
  { status: 'unmatched', label: 'Not identified' },
  { status: 'ambiguous', label: 'Needs review' },
  { status: 'pending', label: 'Awaiting identification' },
] as const

export function useSourceTorrentStats(enabled: boolean) {
  const query = useQuery({
    queryKey: ['source-torrent-stats'],
    queryFn: ({ signal }) => api<Items<TorrentStats>>('/source-torrent-stats', { signal }),
    enabled,
    refetchInterval: enabled ? 15_000 : false,
  })
  const bySource = useMemo(
    () => new Map(query.data?.items?.map((item) => [item.provider_id, item])),
    [query.data],
  )
  return { ...query, bySource }
}

export function SourceTorrentStats({
  source,
  counts,
  unavailable,
}: {
  source: string
  counts?: TorrentStats
  unavailable: boolean
}) {
  if (unavailable || !counts)
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
          <dd className="mt-1">
            <Link
              className="data-link font-medium tabular-nums"
              to={`/torrents?${params({ provider: counts.provider_id })}`}
              aria-label={`View ${number(counts.imported)} imported torrents from ${source}`}
            >
              {number(counts.imported)}
            </Link>
          </dd>
        </div>
        {decisions.map(({ status, label }) => (
          <div key={status}>
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1">
              <Link
                className="data-link tabular-nums"
                to={`/metadata/identification?${params({ match_status: status, match_provider: counts.provider_id })}`}
                aria-label={`View ${number(counts[status])} ${label.toLowerCase()} torrents from ${source}`}
              >
                {number(counts[status])}
              </Link>
            </dd>
          </div>
        ))}
      </dl>
      {counts.pending_import && (
        <p className="help border-t pt-2">
          <Link
            className="data-link tabular-nums"
            to={`/runs/${encodeURIComponent(counts.pending_import.run_id)}`}
          >
            {number(counts.pending_import.torrents)} awaiting import completion
          </Link>
        </p>
      )}
    </div>
  )
}
