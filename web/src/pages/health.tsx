import { useQuery } from '@tanstack/react-query'
import { api, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { bytes, useDate } from '@/lib/format'
import type { SystemHealth } from '@/lib/health-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { storageStatus } from '@/lib/storage-status'
import { StorageSettingsPanel } from '@/components/storage-settings'
import { StorageBreakdown } from '@/components/storage-breakdown'

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
            <StorageBreakdown report={report} />
          </>
        )
      )}
    </div>
  )
}

export function StorageSettingsPage() {
  return (
    <div className="page">
      <StorageSettingsPanel />
    </div>
  )
}
