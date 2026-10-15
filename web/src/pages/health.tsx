import { useQuery } from '@tanstack/react-query'
import { api, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { bytes, percentage, useDate } from '@/lib/format'
import type { SystemHealth } from '@/lib/health-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { storageStatus } from '@/lib/storage-status'
import { StorageSettingsPanel } from '@/components/storage-settings'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const categories: Record<string, string> = {
  sources: 'Sources',
  metadata: 'Metadata',
  debrid: 'Debrid',
  stremio: 'Stremio',
  sharing: 'Catalog sharing',
  access: 'Security',
  logs: 'Logs and operation history',
  other: 'Settings and database state',
  overhead: 'Database overhead',
}

export function HealthPage() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['system-health'],
    queryFn: ({ signal }) => api<SystemHealth>('/system-health', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const report = query.data
  const stale =
    Boolean(query.error) ||
    Boolean(report && Date.now() - Date.parse(report.checked_at) > 3 * 60_000)
  const database =
    report && storageStatus(report.database.available, report.database.measured_at, stale)
  const disk = report && storageStatus(report.disk.available, report.disk.measured_at, stale)
  return (
    <div className="page">
      <StorageSettingsPanel />
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading storage measurements" />
      ) : (
        report && (
          <>
            <div className="grid items-stretch gap-5 lg:grid-cols-2">
              <section className="section-card">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h2 className="section-title">PostgreSQL database</h2>
                  <Badge variant={database?.variant}>{database?.label}</Badge>
                </div>
                {report.database.measured_at ? (
                  <>
                    <p className="text-2xl font-semibold tabular-nums">
                      {bytes(report.database.total_bytes)}
                    </p>
                    <p className="help">Measured {date(report.database.measured_at)}.</p>
                  </>
                ) : (
                  <p className="help">No database measurement available yet.</p>
                )}
                {!report.database.available && (
                  <p className="notice notice-warning" role="status">
                    The database could not be measured. Earlier values remain visible and may be out
                    of date.
                  </p>
                )}
              </section>
              <section className="section-card">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h2 className="section-title">Local available space</h2>
                  {disk?.label !== 'Available' && (
                    <Badge variant={disk?.variant}>{disk?.label}</Badge>
                  )}
                </div>
                {report.disk.available ? (
                  <>
                    <p className="text-2xl font-semibold tabular-nums">
                      {bytes(report.disk.available_bytes)}{' '}
                      <span className="text-sm font-normal text-muted-foreground">
                        available of {bytes(report.disk.total_bytes)}
                      </span>
                    </p>
                    <p className="help">Measured {date(report.disk.measured_at)}.</p>
                  </>
                ) : (
                  <p className="notice notice-warning" role="status">
                    Local filesystem measurement is unavailable.
                  </p>
                )}
                <p className="help">
                  Space available to AIOSources on this server. PostgreSQL may use another volume or
                  server.
                </p>
              </section>
            </div>
            <section className="section-card">
              <h2 className="section-title">Where storage is used</h2>
              {report.database.measured_at ? (
                <>
                  <div className="table-frame">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Stored data</TableHead>
                          <TableHead className="text-right">Space used</TableHead>
                          <TableHead className="text-right">Share of database</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {[
                          ...report.database.categories,
                          { key: 'overhead', bytes: report.database.other_database_bytes },
                        ]
                          .sort((a, b) => b.bytes - a.bytes || a.key.localeCompare(b.key))
                          .map((category) => (
                            <TableRow key={category.key}>
                              <TableCell>{categories[category.key] || category.key}</TableCell>
                              <TableCell className="text-right font-mono text-xs">
                                {bytes(category.bytes)}
                              </TableCell>
                              <TableCell className="text-right tabular-nums text-xs">
                                {category.bytes >= 0
                                  ? percentage(category.bytes, report.database.total_bytes)
                                  : 'Not available'}
                              </TableCell>
                            </TableRow>
                          ))}
                      </TableBody>
                    </Table>
                  </div>
                  <p className="help">
                    Tables and indexes are included. Sizes are measured independently and may
                    briefly differ while imports are running.
                  </p>
                </>
              ) : (
                <p className="help">
                  Storage usage will appear after the first successful database measurement.
                </p>
              )}
            </section>
          </>
        )
      )}
    </div>
  )
}
