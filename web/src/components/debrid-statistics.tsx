import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { number, percentage, useDate } from '@/lib/format'
import type { Config } from '@/lib/aiosource-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Badge } from '@/components/ui/badge'

type CacheStats = {
  service: string
  cached: number
  uncached: number
  last_checked: string
  checks?: { cached: number; uncached: number; last_checked: string | null }
}

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
          <section className="space-y-4" aria-label="Availability checks">
            <h2 className="section-title">Availability checks</h2>
            <div className="grid gap-5 md:grid-cols-2">
              {(
                [
                  ['alldebrid', 'AllDebrid', Boolean(config.alldebrid_secret)],
                  ['torbox', 'TorBox', Boolean(config.torbox_secret)],
                ] as const
              ).map(([service, name, configured]) => {
                const item = query.data.items.find((item) => item.service === service)
                const counts = item?.checks ?? item
                const cached = counts?.cached ?? 0,
                  uncached = counts?.uncached ?? 0,
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
                        ? `${number(total)} ${item?.checks ? 'account checks' : 'shared observations'} · Last check: ${date(counts?.last_checked ?? undefined)}`
                        : 'No checks in the current cache period.'}
                    </p>
                    {item?.checks && (
                      <p className="help">
                        Shared cache: {number(item.cached)} cached · {number(item.uncached)} not
                        cached
                      </p>
                    )}
                    <Link className="data-link text-sm" to={`/debrid/history?service=${service}`}>
                      View history
                    </Link>
                  </section>
                )
              })}
            </div>
            <p className="help">
              Last known result per torrent and service within the cache period; not the percentage
              of your catalog available at the provider. Negative checks may need rechecking sooner.
              Shared cache counts include only reusable observations.
            </p>
          </section>
        )
      )}
    </>
  )
}
