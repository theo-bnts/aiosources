import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import type { Status } from '@/lib/aiosource-types'
import { identificationResults, identificationStatuses } from '@/lib/identification'
import {
  identificationCorrespondencesPath,
  identificationResultsPath,
} from '@/lib/metadata-navigation'
import { number, percentage, useDate } from '@/lib/format'
import { ErrorNotice, Loading } from '@/components/common'
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
  const notice = <ErrorNotice error={query.error} retry={() => void query.refetch()} />
  if (!status)
    return (
      <div className="page">
        {notice}
        {query.isPending && <Loading label="Loading statistics" />}
      </div>
    )
  const total =
    status.status.matched +
    status.status.ambiguous +
    status.status.unmatched +
    status.status.matching_pending
  const next = status.automation.next_identification
  return (
    <section className="page" aria-label="Identification statistics">
      {notice}
      <dl className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <Statistic label="Imported torrents" value={total} to={identificationResultsPath} />
        {identificationStatuses.map((decision) => (
          <Statistic
            key={decision}
            label={
              decision === 'matched' ? 'Identified torrents' : identificationResults[decision].label
            }
            value={`${number(status.status[decision === 'pending' ? 'matching_pending' : decision])} (${percentage(status.status[decision === 'pending' ? 'matching_pending' : decision], total)})`}
            to={`${identificationResultsPath}?match_status=${decision}`}
          />
        ))}
      </dl>
      <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Statistic
          label="Correspondence groups"
          value={status.status.correspondence_groups ?? 0}
          to={identificationCorrespondencesPath}
          help="Verified release groups linking identical torrents across multiple sources."
        />
        <Statistic
          label="Linked torrents"
          value={status.status.correspondence_members ?? 0}
          to={identificationCorrespondencesPath}
        />
        <Statistic
          label="Enriched torrents"
          value={status.status.correspondence_enriched ?? 0}
          to={`${identificationCorrespondencesPath}?corr_enriched=true`}
          help="Torrents receiving additional audio or subtitle languages from verified cross-source peers."
        />
        <Statistic
          label="Pending correspondences"
          value={status.status.correspondences_pending ?? 0}
          to={identificationCorrespondencesPath}
        />
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
