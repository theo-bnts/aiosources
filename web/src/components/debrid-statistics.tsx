import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { number, percentage, useDate } from '@/lib/format'
import type { Config } from '@/lib/aiosource-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Badge } from '@/components/ui/badge'

type CacheStats = { service: string; cached: number; uncached: number; last_checked: string }

export function DebridStatistics({ config }: { config: Config }) {
  const date = useDate()
  const query = useQuery({
    queryKey: ['debrid-statistics'],
    queryFn: ({ signal }) => api<{ items: CacheStats[] }>('/debrid/statistics', { signal }),
    refetchInterval: COLLECTION_REFRESH_INTERVAL,
  })
  return (
    <>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading cache statistics" />
      ) : (
        query.data && (
          <section className="space-y-4" aria-label="Shared availability cache">
            <h2 className="section-title">Shared availability cache</h2>
            <div className="grid gap-5 md:grid-cols-2">
              {(
                [
                  ['alldebrid', 'AllDebrid', Boolean(config.alldebrid_secret)],
                  ['torbox', 'TorBox', Boolean(config.torbox_secret)],
                ] as const
              ).map(([service, name, configured]) => {
                const item = query.data.items.find((item) => item.service === service)
                const cached = item?.cached ?? 0,
                  uncached = item?.uncached ?? 0,
                  total = cached + uncached
                return (
                  <section className="section-card" key={service}>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <h3 className="section-title mb-0">{name}</h3>
                      <Badge variant={configured ? 'success' : 'secondary'}>
                        {configured ? 'Configured' : 'Not configured'}
                      </Badge>
                    </div>
                    <dl className="grid grid-cols-2 gap-4 text-sm">
                      <div>
                        <dt className="text-muted-foreground">Cached</dt>
                        <dd className="mt-1 tabular-nums text-primary">
                          {number(cached)} ({percentage(cached, total)})
                        </dd>
                      </div>
                      <div>
                        <dt className="text-muted-foreground">Not cached</dt>
                        <dd className="mt-1 tabular-nums">
                          {number(uncached)} ({percentage(uncached, total)})
                        </dd>
                      </div>
                    </dl>
                    <p className="help">
                      {total
                        ? `${number(total)} valid observations · Last check: ${date(item?.last_checked)}`
                        : 'No valid shared observations yet.'}
                    </p>
                    <Link className="data-link text-sm" to={`/debrid/history?service=${service}`}>
                      View history
                    </Link>
                  </section>
                )
              })}
            </div>
            <p className="help">
              Only reusable observations within the current cache lifetime. Unchecked and expired
              torrents are excluded.
            </p>
          </section>
        )
      )}
    </>
  )
}
