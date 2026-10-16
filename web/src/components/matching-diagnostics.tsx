import { ResultCount } from '@/components/common'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, params, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { useSourceNames } from '@/lib/source-names'
import type { MatchDiagnostic } from '@/lib/aiosource-types'
import type { Items, Page, ProviderSummary } from '@/lib/types'
import { useURLPagination } from '@/lib/pagination'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { useDate } from '@/lib/format'
import { identificationResults } from '@/lib/identification'
import { ClearFiltersButton, ListEmpty, ErrorNotice, Loading, Pager } from '@/components/common'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { IdentificationReview } from '@/components/identification-review'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'

const reasons: Record<string, string> = {
  'manually reviewed': 'Film or series chosen in a manual review.',
  'reviewed source changed':
    'Source data changed after a manual review. Check and confirm the current identification.',
  'reviewed information unavailable':
    'The reviewed film or series is no longer available. Choose a current entry.',
  'multiple plausible works':
    'Several films or series are plausible. Check the title, year and source identifiers; a match will not be forced.',
  'no supported correspondence':
    'No reliable correspondence in the downloaded information. Update it, then check the source title, year and identifiers.',
  'source season conflict': 'The source season disagrees with the release title.',
  'source episode conflict': 'The source episode disagrees with the release title.',
  'media type conflict': 'Film or series information disagrees with the source.',
  'year conflict': 'The release year disagrees with the downloaded information.',
  'numbered title conflict': 'A number in the title disagrees with the candidate title.',
  'source ID conflict': 'Source identifiers disagree with the downloaded information.',
  'source ID unverified': 'The downloaded information cannot verify a source identifier.',
  'insufficient evidence':
    'The title alone is not enough to identify this release safely. Check its year and identifiers.',
  'awaiting identification':
    'New or changed information needs identification. Run Identify torrents or enable automatic identification.',
  'exact alias with compatible evidence': 'Title and supporting information agree.',
  'close alias corroborated by year and source ID':
    'A close title is confirmed by its year and source identifier.',
  'exact alias disambiguated by compatible source ID':
    'A source identifier distinguishes this title from other works with the same name.',
}

export function MatchingDiagnostics() {
  const sourceName = useSourceNames()
  const [reviewing, setReviewing] = useState<MatchDiagnostic | null>(null)
  const [search, setSearch] = useViewSearchParams()
  const status = search.get('match_status') ?? 'ambiguous'
  const provider = search.get('match_provider') ?? ''
  const q = search.get('match_q') ?? ''
  const pagination = useURLPagination({ limitKey: 'match_limit', offsetKey: 'match_offset' })
  const { limit, offset } = pagination
  const date = useDate()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const records = useQuery({
    queryKey: ['matching-diagnostics', { status, provider, q, limit, offset }],
    queryFn: ({ signal }) =>
      api<Page<MatchDiagnostic>>(
        `/aiosource/matches?${params({ status: status === 'all' ? '' : status, provider, q, limit, offset })}`,
        { signal },
      ),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  function filter(patch: Record<string, string>) {
    const next = new URLSearchParams(search)
    next.delete('match_offset')
    for (const [key, value] of Object.entries(patch))
      value ? next.set(key, value) : next.delete(key)
    setSearch(next)
  }
  return (
    <div className="page" id="matching-results">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field">
          <Label htmlFor="matching-status">Identification result</Label>
          <select
            id="matching-status"
            className="native-select"
            value={status}
            onChange={(event) => filter({ match_status: event.target.value })}
          >
            <option value="all">All results</option>
            {Object.entries(identificationResults).map(([key, { label }]) => (
              <option key={key} value={key}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <Label htmlFor="matching-source">Source</Label>
          <select
            id="matching-source"
            className="native-select"
            value={provider}
            onChange={(event) => filter({ match_provider: event.target.value })}
          >
            <option value="">All sources</option>
            {provider && !providers.data?.items?.some((item) => item.id === provider) && (
              <option value={provider}>{provider}</option>
            )}
            {providers.data?.items?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name || item.id}
              </option>
            ))}
          </select>
        </div>
      </div>
      <form
        className="toolbar"
        onSubmit={(event) => {
          event.preventDefault()
          filter({ match_q: String(new FormData(event.currentTarget).get('q') ?? '').trim() })
        }}
      >
        <div className="field min-w-0 basis-full sm:basis-auto sm:flex-1">
          <Label htmlFor="matching-search">Title, source ID or info hash</Label>
          <Input key={q} id="matching-search" name="q" defaultValue={q} />
        </div>
        <Button type="submit" variant="outline" className="self-end">
          Search
        </Button>
        <div className="self-end">
          <ClearFiltersButton
            active={status !== 'all' || Boolean(provider || q)}
            onClear={() => filter({ match_status: 'all', match_provider: '', match_q: '' })}
          />
        </div>
      </form>
      <ErrorNotice
        error={records.error ?? providers.error}
        retry={() => {
          void records.refetch()
          void providers.refetch()
        }}
      />
      {records.isPending ? (
        <Loading />
      ) : (
        records.data && (
          <>
            <ResultCount total={records.data.total} label="torrents" />
            {records.data.items?.length ? (
              <>
                <div
                  role="list"
                  aria-label="Identification results"
                  className="space-y-3 md:hidden"
                >
                  {records.data.items?.map((item) => (
                    <article
                      role="listitem"
                      key={item.occurrence_id}
                      className="space-y-3 rounded-md border p-4"
                    >
                      <div className="min-w-0">
                        <Link
                          className="data-link break-words font-medium"
                          to={`/torrents?occurrence=${encodeURIComponent(item.occurrence_id)}`}
                        >
                          {item.title || item.source_id}
                        </Link>
                        <p className="help mt-1 break-words">
                          {sourceName(item.provider_id)} · {item.source_id}
                        </p>
                      </div>
                      <p className="text-sm font-medium">
                        <Badge variant={identificationResults[item.status].variant}>
                          {identificationResults[item.status].label}
                        </Badge>
                        {item.imdb && <span className="mono ml-2">{item.imdb}</span>}
                      </p>
                      <p className="help">
                        {reasons[item.reason] ??
                          'Review the source information, then run identification again.'}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        Last identified: {item.updated_at ? date(item.updated_at) : 'Not current'}
                      </p>
                      <Button
                        variant="outline"
                        onClick={() => setReviewing(item)}
                        aria-label={`Review ${item.title || item.source_id}`}
                      >
                        Review
                      </Button>
                    </article>
                  ))}
                </div>
                <div className="table-frame hidden md:block">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Torrent / source</TableHead>
                        <TableHead>Result</TableHead>
                        <TableHead>Explanation</TableHead>
                        <TableHead>Last identified</TableHead>
                        <TableHead>
                          <span className="sr-only">Review</span>
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {records.data.items?.map((item) => (
                        <TableRow key={item.occurrence_id}>
                          <TableCell className="min-w-48 max-w-md whitespace-normal break-words">
                            <Link
                              className="data-link font-medium"
                              to={`/torrents?occurrence=${encodeURIComponent(item.occurrence_id)}`}
                            >
                              {item.title || item.source_id}
                            </Link>
                            <p className="help mt-1">
                              {sourceName(item.provider_id)} · {item.source_id}
                            </p>
                          </TableCell>
                          <TableCell className="whitespace-normal">
                            <Badge variant={identificationResults[item.status].variant}>
                              {identificationResults[item.status].label}
                            </Badge>
                            {item.imdb && <p className="mono mt-1">{item.imdb}</p>}
                          </TableCell>
                          <TableCell className="min-w-64 max-w-xl whitespace-normal">
                            {reasons[item.reason] ??
                              'Review the source information, then run identification again.'}
                          </TableCell>
                          <TableCell className="text-xs text-muted-foreground">
                            {item.updated_at ? date(item.updated_at) : 'Not current'}
                          </TableCell>
                          <TableCell>
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => setReviewing(item)}
                              aria-label={`Review ${item.title || item.source_id}`}
                            >
                              Review
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </>
            ) : (
              <ListEmpty
                title="No torrents in this view"
                description="Choose another result or source, or clear the search. Newly imported torrents appear as awaiting identification."
                filtered={status !== 'all' || Boolean(provider || q)}
                offset={offset}
                onFirstPage={() => pagination.setOffset(0)}
              />
            )}
            <Pager
              total={records.data.total}
              limit={records.data.limit}
              offset={offset}
              onChange={pagination.setOffset}
              busy={records.isFetching}
            />
          </>
        )
      )}
      {reviewing && (
        <IdentificationReview
          key={reviewing.occurrence_id}
          item={reviewing}
          onClose={() => setReviewing(null)}
        />
      )}
    </div>
  )
}
