import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Empty, ErrorNotice, Loading } from '@/components/common'
import { useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Statistic } from '@/components/statistic'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api, params } from '@/lib/api'
import { number, percentage } from '@/lib/format'
import { compareListNames } from '@/lib/list-view'
import { filesExplorerPath } from '@/lib/source-navigation'
import type { Items, ProviderSummary } from '@/lib/types'

export default function FilesStatisticsPage() {
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const counts = useSourceTorrentStats(true)
  const total = counts.totals?.imported
  const rows = (providers.data?.items ?? [])
    .map((source) => ({
      id: source.id,
      name: source.name || source.id,
      remote: source.remote_catalog,
      imported: counts.bySource.get(source.id)?.imported ?? 0,
    }))
    .filter(({ imported }) => imported > 0)
    .sort(compareListNames)
  let local = 0
  let remote = 0
  for (const row of rows) {
    if (row.remote) remote += row.imported
    else local += row.imported
  }
  return (
    <div className="page">
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      <ErrorNotice error={counts.error} retry={() => void counts.refetch()} />
      {(counts.isPending || providers.isPending) && <Loading label="Loading file statistics" />}
      {total !== undefined && providers.data && (
        <>
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <Statistic
              className="col-span-2 sm:col-span-1"
              label="Imported files"
              value={total}
              to={filesExplorerPath}
              help="Each published record counts once per source. The same torrent can appear in multiple sources."
            />
            <Statistic label="From local sources" value={local} />
            <Statistic label="From remote catalogs" value={remote} />
          </dl>
          {rows.length > 0 && (
            <section className="section-card" aria-labelledby="files-by-source">
              <h2 className="section-title" id="files-by-source">
                Files by source
              </h2>
              <div className="rounded-md border">
                <Table aria-labelledby="files-by-source" className="table-fixed">
                  <TableHeader>
                    <TableRow>
                      <TableHead scope="col">Source</TableHead>
                      <TableHead scope="col" className="w-24 text-right">
                        Files
                      </TableHead>
                      <TableHead scope="col" className="w-20 text-right">
                        Share
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((row) => (
                      <TableRow key={row.id}>
                        <TableCell className="whitespace-normal [overflow-wrap:anywhere]">
                          <Link
                            className="data-link"
                            to={`${filesExplorerPath}?${params({ provider: row.id })}`}
                          >
                            {row.name}
                          </Link>
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {number(row.imported)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {percentage(row.imported, total)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </section>
          )}
          {total === 0 && (
            <Empty
              title="No imported files"
              description="Files appear here when a source import or remote catalog sync publishes them."
            />
          )}
        </>
      )}
    </div>
  )
}
