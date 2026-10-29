import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import type { Status } from '@/lib/aiosource-types'
import { identificationResults, identificationStatuses } from '@/lib/identification'
import { number, percentage, useDate } from '@/lib/format'
import { ErrorNotice } from '@/components/common'
import { Statistic } from '@/components/statistic'
import { Badge } from '@/components/ui/badge'

export function IdentificationStatistics() {
  const date = useDate()
  const query = useQuery({
    queryKey: ['aiosource'],
    queryFn: ({ signal }) => api<Status>('/aiosource', { signal }),
    refetchInterval: COLLECTION_REFRESH_INTERVAL,
  })
  const status = query.data
  if (!status) return <ErrorNotice error={query.error} retry={() => void query.refetch()} />
  const total =
    status.status.matched +
    status.status.ambiguous +
    status.status.unmatched +
    status.status.matching_pending
  const next = status.automation.next_identification
  return (
    <section className="space-y-4" aria-label="Identification statistics">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      <dl className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Statistic
          label="Imported torrents"
          value={total}
          to="/metadata/identification?match_status=all"
        />
        {identificationStatuses.map((decision) => (
          <Statistic
            key={decision}
            label={
              decision === 'matched' ? 'Identified torrents' : identificationResults[decision].label
            }
            value={`${number(status.status[decision === 'pending' ? 'matching_pending' : decision])} (${percentage(status.status[decision === 'pending' ? 'matching_pending' : decision], total)})`}
            to={`/metadata/identification?match_status=${decision}`}
          />
        ))}
      </dl>
      <dl className="grid gap-3 sm:grid-cols-2">
        <Statistic
          label="Last identification"
          value={
            status.status.last_match
              ? date(status.status.last_match)
              : 'No completed identification'
          }
          size="sm"
          to="/metadata/identification/history"
        />
        <Statistic
          label="Next automatic identification"
          value={
            status.matching
              ? 'Running'
              : next
                ? date(next)
                : !status.config.auto_match
                  ? 'Disabled'
                  : status.automation.identification === 'up_to_date'
                    ? 'Up to date'
                    : 'Waiting for information'
          }
          size="sm"
          to="/metadata/identification/settings"
        />
      </dl>
      <Link to="/metadata/identification/history" aria-label="View identification history">
        <Badge variant={status.matching ? 'info' : status.matching_error ? 'warning' : 'secondary'}>
          {status.matching
            ? 'Identification running'
            : status.matching_error
              ? 'Identification needs attention'
              : 'Identification idle'}
        </Badge>
      </Link>
    </section>
  )
}
