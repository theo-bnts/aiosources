import { useQuery } from '@tanstack/react-query'
import { MagnifyingGlassIcon } from '@phosphor-icons/react'
import { api, params } from '@/lib/api'
import { publicAPI } from '@/lib/public-credentials'
import { usePublicSearch } from '@/lib/public-search'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { useURLPagination } from '@/lib/pagination'
import { browserLanguage } from '@/lib/language-preferences'
import { readResultSorting } from '@/lib/result-preferences'
import type { CatalogRelease } from '@/lib/catalog-search-types'
import type { Items, Page, ProviderSummary } from '@/lib/types'
import { CatalogReleaseList } from '@/components/catalog-release-list'
import { SourceMultiSelect } from '@/components/source-multi-select'
import { ResultSorting, ResponseTimeout } from '@/components/result-controls'
import {
  ClearFiltersButton,
  Empty,
  ErrorNotice,
  Loading,
  PageHeader,
  Pager,
  ResultCount,
} from '@/components/common'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Button } from '@/components/ui/button'

export function CatalogTorrentsPage() {
  const visitor = usePublicSearch()
  const searchPath = visitor ? '/public/search' : '/search'
  const [search, setSearch] = useViewSearchParams()
  const pagination = useURLPagination()
  const q = search.get('q') || ''
  const discloseSources = !visitor || Boolean(visitor.info.sources)
  const providers = discloseSources ? (search.get('provider') || '').split(',').filter(Boolean) : []
  const cachedOnly = search.get('cached_only') === 'true'
  const sorting = readResultSorting(search.get('sorting')).filter(
    (rule) => rule.field !== 'language' && (discloseSources || rule.field !== 'source'),
  )
  const responseSeconds = Number(search.get('response_seconds')) || undefined
  const sources = useQuery({
    queryKey: ['providers'],
    enabled: !visitor,
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const sourceOptions = visitor
    ? (visitor.info.sources ?? []).map((source) => ({ value: source.id, label: source.name }))
    : (sources.data?.items ?? []).map((source) => ({ value: source.id, label: source.name }))
  const queryParams = params({
    q,
    provider: providers.join(','),
    cached_only: String(cachedOnly),
    sorting: sorting.length ? JSON.stringify(sorting) : '',
    response_seconds: responseSeconds,
    language: browserLanguage(),
    limit: pagination.limit,
    offset: pagination.offset,
  })
  const query = useQuery({
    queryKey: visitor
      ? ['public-catalog', visitor.scope, 'torrents', queryParams]
      : ['catalog-torrents', queryParams],
    enabled: q.trim().length >= 2,
    queryFn: ({ signal }) =>
      visitor
        ? publicAPI<Page<CatalogRelease>>(
            `/catalog/torrents?${queryParams}`,
            visitor.configuration,
            { signal },
            visitor.credential,
          )
        : api<Page<CatalogRelease>>(`/catalog/torrents?${queryParams}`, { signal }),
  })
  function filter(key: string, value: string) {
    const next = new URLSearchParams(search)
    value ? next.set(key, value) : next.delete(key)
    next.delete('offset')
    setSearch(next, { flushSync: true, preventScrollReset: true })
  }
  return (
    <div className={visitor ? 'space-y-6' : 'page'}>
      <PageHeader title="Search torrents" back={{ to: searchPath, label: 'Search' }} />
      <form
        key={q}
        className="space-y-4 rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          const data = new FormData(event.currentTarget)
          filter('q', String(data.get('q') || '').trim())
        }}
      >
        <div className="field">
          <Label
            htmlFor="direct-torrent-query"
            help="Search published torrents directly, including releases without a matching film, series or book record."
          >
            Title or info hash
          </Label>
          <Input
            id="direct-torrent-query"
            name="q"
            type="search"
            defaultValue={q}
            minLength={2}
            maxLength={500}
            required
            placeholder="Search torrent titles or info hashes"
          />
        </div>
        <Disclosure title="Filters">
          <div className="flex max-w-sm items-center justify-between gap-3">
            <Label htmlFor="direct-torrent-cached">Cached results only</Label>
            <Switch
              id="direct-torrent-cached"
              checked={cachedOnly}
              onCheckedChange={(checked) => filter('cached_only', checked ? 'true' : '')}
            />
          </div>
          {discloseSources && (
            <div className="field max-w-sm">
              <Label htmlFor="direct-torrent-sources">Sources</Label>
              <SourceMultiSelect
                id="direct-torrent-sources"
                value={providers}
                options={sourceOptions}
                onChange={(value) => filter('provider', value.join(','))}
              />
            </div>
          )}
          <ResponseTimeout
            value={responseSeconds}
            onChange={(value) => filter('response_seconds', value ? String(value) : '')}
          />
        </Disclosure>
        <div className="flex flex-wrap gap-2">
          <Button id="direct-torrent-submit" type="submit">
            <MagnifyingGlassIcon />
            Search
          </Button>
          <ClearFiltersButton
            onClear={() => setSearch(new URLSearchParams(), { flushSync: true })}
          />
        </div>
      </form>
      <ResultSorting
        value={sorting}
        onChange={(value) => filter('sorting', value.length ? JSON.stringify(value) : '')}
        sources={discloseSources}
      />
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {q.trim().length < 2 ? (
        <Empty
          title="Find a torrent"
          description="Enter a title or info hash to search published source releases directly."
          icon={MagnifyingGlassIcon}
        />
      ) : query.isPending ? (
        <Loading label="Searching torrents" />
      ) : (
        query.data && (
          <>
            <ResultCount total={query.data.total} label="torrents" />
            {query.data.items?.length ? (
              <CatalogReleaseList
                items={query.data.items ?? []}
                id="torrents"
                discloseSources={discloseSources}
              />
            ) : (
              <Empty
                title="No matching torrents"
                description={
                  visitor
                    ? 'No available source releases match this search and your permitted filters.'
                    : 'No published source releases match this search. Adjust the query or filters.'
                }
              />
            )}
            <Pager
              total={query.data.total}
              limit={query.data.limit}
              offset={query.data.offset}
              onChange={pagination.setOffset}
              busy={query.isFetching}
            />
          </>
        )
      )}
    </div>
  )
}
