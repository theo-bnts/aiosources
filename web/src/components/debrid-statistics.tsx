import { debridProviders } from '@/lib/debrid'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import { number, percentage, useDate } from '@/lib/format'
import type { Config } from '@/lib/aiosource-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Statistic } from '@/components/statistic'
import { Button } from '@/components/ui/button'
import { ArrowRightIcon } from '@phosphor-icons/react'

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
  const totals = (query.data?.items ?? []).reduce(
    (sum, item) => ({
      cached: sum.cached + item.cached,
      uncached: sum.uncached + item.uncached,
      account: sum.account + (item.checks ? item.checks.cached + item.checks.uncached : 0),
    }),
    { cached: 0, uncached: 0, account: 0 },
  )
  return (
    <>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading cache statistics" />
      ) : (
        query.data && (
          <section className="space-y-4" aria-label="Availability checks">
            <dl className="grid grid-cols-2 gap-3 xl:grid-cols-4">
              <Statistic
                label="Configured services"
                value={debridProviders.filter((provider) => config[provider.secret]).length}
              />
              <Statistic
                label="Shared cached observations"
                value={totals.cached}
                detail={`(${percentage(totals.cached, totals.cached + totals.uncached)})`}
              />
              <Statistic
                label="Shared not cached observations"
                value={totals.uncached}
                detail={`(${percentage(totals.uncached, totals.cached + totals.uncached)})`}
              />
              <Statistic label="Account observations" value={totals.account} />
            </dl>
            <div className="grid items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3">
              {debridProviders.map((provider) => {
                const { id: service, name } = provider
                const configured = Boolean(config[provider.secret])
                const item = query.data.items.find((item) => item.service === service)
                const counts = item?.checks ?? item
                const cached = counts?.cached ?? 0,
                  uncached = counts?.uncached ?? 0,
                  total = cached + uncached
                return (
                  <section className="statistic-card flex h-full flex-col gap-4" key={service}>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <h3 className="section-title mb-0">
                        <Link className="data-link" to={`/debrid/settings#${provider.secret}`}>
                          {name}
                        </Link>
                      </h3>
                      <Badge variant={configured ? 'success' : 'secondary'}>
                        {configured ? 'Configured' : 'Not configured'}
                      </Badge>
                    </div>
                    {'hint' in provider && <p className="help">{provider.hint}</p>}
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
                        : 'No verified observations in the current cache period.'}
                    </p>
                    {item?.checks && (
                      <p className="help">
                        Shared cache: {number(item.cached)} cached · {number(item.uncached)} not
                        cached
                      </p>
                    )}
                    <div className="card-actions mt-auto">
                      <Button asChild variant="outline" size="sm">
                        <Link to={`/debrid/history?service=${service}`}>
                          <ArrowRightIcon />
                          View history
                        </Link>
                      </Button>
                    </div>
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
