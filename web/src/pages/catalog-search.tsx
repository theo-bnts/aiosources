import { CatalogCover } from '@/components/catalog-cover'
import { Disclosure } from '@/components/ui/disclosure'
import { ResultSorting, ResponseTimeout } from '@/components/result-controls'
import { readResultSorting } from '@/lib/result-preferences'
import { LanguageFilter } from '@/components/language-filter'
import {
  useLanguagePreferences,
  resolvedLanguage,
  browserLanguage,
} from '@/lib/language-preferences'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { ArrowRightIcon, MagnifyingGlassIcon } from '@phosphor-icons/react'
import { api, params } from '@/lib/api'
import { bytes, number, useDate } from '@/lib/format'
import { usePublicSearch } from '@/lib/public-search'
import { publicAPI } from '@/lib/public-credentials'
import { useSourceNames } from '@/lib/source-names'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import { useURLPagination } from '@/lib/pagination'
import type { Page } from '@/lib/types'
import type { CatalogTitle, CatalogRelease, CatalogSelection } from '@/lib/catalog-search-types'
import {
  ClearFiltersButton,
  Empty,
  ErrorNotice,
  Loading,
  PageHeader,
  Pager,
  ResultCount,
} from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { DownloadAction } from '@/components/catalog-download'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'

function TitleDetails({ title, compact = true }: { title: CatalogTitle; compact?: boolean }) {
  return (
    <p className={`help break-words ${compact ? 'line-clamp-3' : ''}`}>
      {title.type === 'movie' ? 'Movie' : title.type === 'book' ? 'Book' : 'Series'}
      {!!title.authors?.length && ` · ${title.authors.join(', ')}`}
      {title.years.length > 0 && ` · ${title.years.join(', ')}`}
      {title.imdb && ` · ${title.imdb}`}
    </p>
  )
}

export function CatalogSearchPage() {
  const visitor = usePublicSearch()
  const searchPath = visitor ? '/public/search' : '/search'
  const language = browserLanguage()
  const [search, setSearch] = useViewSearchParams()
  const listState = useListViewState()
  const pagination = useURLPagination()
  const q = search.get('q') || ''
  const kind = search.get('type') || ''
  const year = search.get('year') || ''
  const query = useQuery({
    queryKey: visitor
      ? [
          'public-catalog',
          visitor.scope,
          'search',
          q,
          kind,
          year,
          language,
          pagination.limit,
          pagination.offset,
        ]
      : ['catalog-search', q, kind, year, language, pagination.limit, pagination.offset],
    queryFn: ({ signal }) => {
      const path = `/catalog/search?${params({ q, type: kind, year, language, limit: pagination.limit, offset: pagination.offset })}`
      return visitor
        ? publicAPI<Page<CatalogTitle>>(path, visitor.configuration, { signal }, visitor.credential)
        : api<Page<CatalogTitle>>(path, { signal })
    },
    enabled: q.trim().length >= 2,
  })
  return (
    <div className={visitor ? 'space-y-6' : 'page'}>
      <form
        key={`${q}:${kind}:${year}`}
        className="space-y-4 rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          const data = new FormData(event.currentTarget)
          const next = new URLSearchParams()
          next.set('q', String(data.get('q') || '').trim())
          if (data.get('type')) next.set('type', String(data.get('type')))
          if (data.get('year')) next.set('year', String(data.get('year')))
          setSearch(next, { flushSync: true })
        }}
      >
        <div className="field min-w-48 flex-1">
          <Label htmlFor="catalog-query">Title, author or identifier</Label>
          <Input
            id="catalog-query"
            name="q"
            type="search"
            defaultValue={q}
            minLength={2}
            maxLength={500}
            required
            placeholder="Search movies, series and books"
          />
        </div>
        <Disclosure title="Filters" open>
          <div className="grid items-start gap-4 sm:grid-cols-2">
            <div className="field">
              <Label htmlFor="catalog-type">Type</Label>
              <select
                id="catalog-type"
                name="type"
                className="native-select w-full"
                defaultValue={kind}
              >
                <option value="">All media</option>
                <option value="movie">Movies</option>
                <option value="tv">Series</option>
                <option value="book">Books</option>
                <option value="ebook">Ebooks</option>
                <option value="audiobook">Audiobooks</option>
              </select>
            </div>
            <div className="field min-w-0">
              <Label htmlFor="catalog-year">Year</Label>
              <Input
                id="catalog-year"
                name="year"
                type="number"
                min={1000}
                max={9999}
                step={1}
                defaultValue={year}
                placeholder="Any year"
              />
            </div>
          </div>
        </Disclosure>
        <div className="flex flex-wrap gap-2">
          <Button id="catalog-submit" type="submit">
            <MagnifyingGlassIcon />
            Search
          </Button>
          <ClearFiltersButton
            onClear={() => {
              setSearch(new URLSearchParams(), { flushSync: true })
            }}
          />
        </div>
      </form>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {q.trim().length < 2 ? (
        <Empty
          title="Find a title"
          description="Search your identified catalog, then choose a version to download through your debrid services."
          icon={MagnifyingGlassIcon}
        />
      ) : query.isPending ? (
        <Loading label="Searching titles" />
      ) : (
        query.data && (
          <>
            <ResultCount total={query.data.total} label="titles" />
            {query.data.items?.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Title</TableHead>
                      <TableHead className="text-right">Source releases</TableHead>
                      <TableHead>
                        <span className="sr-only">Open</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {query.data.items?.map((title) => (
                      <TableRow key={title.id}>
                        <TableCell className="whitespace-normal">
                          <div className="flex min-w-0 items-start gap-3">
                            <CatalogCover src={title.cover} book={title.type === 'book'} />
                            <div className="min-w-0">
                              <Link
                                to={`${searchPath}/${title.id}?${params({ q })}`}
                                state={listState}
                                className="font-medium break-words"
                              >
                                {title.title}
                              </Link>
                              <TitleDetails title={title} />
                            </div>
                          </div>
                        </TableCell>
                        <TableCell className="text-right mono">{number(title.releases)}</TableCell>
                        <TableCell>
                          <Button asChild size="icon-sm" variant="ghost">
                            <Link
                              to={`${searchPath}/${title.id}?${params({ q })}`}
                              state={listState}
                              aria-label={`Show releases for ${title.title}`}
                            >
                              <ArrowRightIcon />
                            </Link>
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <Empty
                title="No matching titles"
                description={
                  visitor
                    ? 'No identified titles match this search.'
                    : 'Only titles with currently identified source releases appear here. Check Sources and Metadata if an imported title is missing.'
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

export function CatalogTitlePage({ publicID }: { publicID?: string }) {
  const visitor = usePublicSearch()
  const searchPath = visitor ? '/public/search' : '/search'
  const [preferences, setPreferences] = useLanguagePreferences()
  const language = resolvedLanguage(preferences)
  const titleLanguage = browserLanguage()
  const { id: routeID = '' } = useParams()
  const id = publicID ?? routeID
  const [search, setSearch] = useViewSearchParams()
  const pagination = useURLPagination()
  const privateSourceName = useSourceNames(!visitor)
  const sourceName = (id?: string, savedName?: string) =>
    visitor
      ? visitor.info.sources?.find((source) => source.id === id)?.name || savedName || id || ''
      : privateSourceName(id, savedName)
  const date = useDate()
  const title = useQuery({
    queryKey: visitor
      ? ['public-catalog', visitor.scope, 'title', id, titleLanguage, search.get('q')]
      : ['catalog-title', id, titleLanguage, search.get('q')],
    queryFn: ({ signal }) => {
      const path = `/catalog/${id}?${params({ language: titleLanguage, q: search.get('q') || '' })}`
      return visitor
        ? publicAPI<CatalogTitle>(path, visitor.configuration, { signal }, visitor.credential)
        : api<CatalogTitle>(path, { signal })
    },
  })
  const selection: CatalogSelection =
    title.data?.type === 'tv'
      ? {
          season: Number(search.get('season') ?? title.data.selection.season ?? 1),
          episode: Number(search.get('episode') ?? title.data.selection.episode ?? 1),
        }
      : {}
  const discloseSources = !visitor || Boolean(visitor.info.sources)
  const sort =
    !discloseSources && search.get('sort') === 'source'
      ? 'size_desc'
      : search.get('sort') || 'size_desc'
  const sorting = search.has('sorting')
    ? readResultSorting(search.get('sorting'))
    : readResultSorting(
        search.has('sort') && sort !== 'size_desc'
          ? JSON.stringify([
              {
                field: sort === 'source' ? 'source' : 'published',
                direction: sort === 'source' ? 'asc' : 'desc',
              },
            ])
          : null,
      )
  const responseSeconds = Number(search.get('response_seconds')) || undefined
  const cachedOnly = search.get('cached_only') === 'true'
  const provider = discloseSources ? search.get('provider') || '' : ''
  const query = params({
    ...selection,
    language,
    language_mode: preferences.mode,
    sort,
    sorting: search.get('sorting') || '',
    response_seconds: responseSeconds,
    provider,
    cached_only: String(cachedOnly),
    limit: pagination.limit,
    offset: pagination.offset,
  })
  const releases = useQuery({
    queryKey: visitor
      ? ['public-catalog', visitor.scope, 'releases', id, query]
      : ['catalog-releases', id, query],
    queryFn: ({ signal }) =>
      visitor
        ? publicAPI<Page<CatalogRelease>>(
            `/catalog/${id}/releases?${query}`,
            visitor.configuration,
            { signal },
            visitor.credential,
          )
        : api<Page<CatalogRelease>>(`/catalog/${id}/releases?${query}`, { signal }),
    enabled: Boolean(title.data),
  })
  const sourceOptions = [...(title.data?.providers ?? [])].sort((a, b) =>
    sourceName(a).localeCompare(sourceName(b)),
  )
  if (provider && !sourceOptions.includes(provider)) sourceOptions.push(provider)
  function filter(key: string, value: string) {
    const next = new URLSearchParams(search)
    if (key === 'sorting') next.delete('sort')
    value ? next.set(key, value) : next.delete(key)
    next.delete('offset')
    setSearch(next, { flushSync: true, preventScrollReset: true })
  }
  return (
    <div className={visitor ? 'space-y-6' : 'page'}>
      <PageHeader
        title={title.data?.title || 'Source releases'}
        back={{ to: searchPath, label: 'Search' }}
      />
      <ErrorNotice error={title.error} retry={() => void title.refetch()} />
      {title.isPending ? (
        <Loading label="Loading title" />
      ) : (
        title.data && (
          <>
            <div className="flex min-w-0 items-start gap-4">
              <CatalogCover src={title.data.cover} book={title.data.type === 'book'} large />
              <div className="min-w-0 space-y-2">
                <TitleDetails title={title.data} compact={false} />
                {title.data.openlibrary && (
                  <a
                    className="data-link text-sm"
                    href={`https://openlibrary.org/${title.data.openlibrary.endsWith('M') ? 'books' : 'works'}/${title.data.openlibrary}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    View on Open Library
                  </a>
                )}
              </div>
            </div>
            <section aria-label="Source release filters">
              <Disclosure title="Filters" open>
                <div className="flex max-w-sm items-center justify-between gap-3">
                  <Label htmlFor="catalog-cached">Cached results only</Label>
                  <Switch
                    id="catalog-cached"
                    checked={cachedOnly}
                    onCheckedChange={(checked) => filter('cached_only', checked ? 'true' : '')}
                  />
                </div>
                <LanguageFilter
                  value={preferences}
                  sorting={sorting.some((rule) => rule.field === 'language')}
                  onChange={(next) => {
                    setPreferences(next)
                    pagination.setOffset(0)
                  }}
                />
                <div className="flex flex-wrap items-end gap-3">
                  {title.data.type === 'tv' && (
                    <form
                      key={`${selection.season}:${selection.episode}`}
                      className="flex flex-wrap items-end gap-3"
                      onSubmit={(event) => {
                        event.preventDefault()
                        const data = new FormData(event.currentTarget)
                        const next = new URLSearchParams(search)
                        next.set('season', String(data.get('season')))
                        next.set('episode', String(data.get('episode')))
                        next.delete('offset')
                        setSearch(next, { flushSync: true })
                      }}
                    >
                      <div className="field w-24">
                        <Label htmlFor="catalog-season">Season</Label>
                        {title.data.seasons?.length ? (
                          <select
                            id="catalog-season"
                            name="season"
                            className="native-select"
                            defaultValue={selection.season}
                          >
                            {!title.data.seasons.includes(selection.season ?? 1) && (
                              <option value={selection.season}>{selection.season}</option>
                            )}
                            {title.data.seasons.map((season) => (
                              <option key={season} value={season}>
                                {season}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <Input
                            id="catalog-season"
                            name="season"
                            type="number"
                            min={0}
                            max={999}
                            required
                            defaultValue={selection.season}
                          />
                        )}
                      </div>
                      <div className="field w-24">
                        <Label htmlFor="catalog-episode">Episode</Label>
                        <Input
                          id="catalog-episode"
                          name="episode"
                          type="number"
                          min={1}
                          max={9999}
                          required
                          defaultValue={selection.episode}
                        />
                      </div>
                      <Button id="catalog-episode-submit" type="submit" variant="outline">
                        Show episode
                      </Button>
                    </form>
                  )}
                  {discloseSources && (
                    <div className="field">
                      <Label htmlFor="release-source">Source</Label>
                      <select
                        id="release-source"
                        className="native-select"
                        value={provider}
                        onChange={(event) => filter('provider', event.target.value)}
                      >
                        <option value="">All sources</option>
                        {sourceOptions.map((value) => (
                          <option key={value} value={value}>
                            {sourceName(value)}
                          </option>
                        ))}
                      </select>
                    </div>
                  )}
                </div>
                <ResponseTimeout
                  value={responseSeconds}
                  onChange={(value) => filter('response_seconds', value ? String(value) : '')}
                />
              </Disclosure>
            </section>
            <ResultSorting
              value={sorting}
              onChange={(next) => filter('sorting', next.length ? JSON.stringify(next) : '')}
              language
              sources={discloseSources}
            />
            <ErrorNotice error={releases.error} retry={() => void releases.refetch()} />
            {releases.isPending ? (
              <Loading label="Checking source releases" />
            ) : (
              releases.data && (
                <>
                  <ResultCount total={releases.data.total} label="source releases" />
                  {!visitor &&
                    releases.data.items?.some((release) =>
                      release.services.some(
                        (service) =>
                          !service.can_add && ['unknown', 'uncached'].includes(service.state),
                      ),
                    ) && (
                      <p className="help">
                        Private sources without an announce URL allow cached downloads only. Add
                        your announce URL in <Link to="/debrid/settings">Debrid settings</Link> to
                        enable uncached downloads.
                      </p>
                    )}
                  {releases.data.items?.length ? (
                    <div className="table-frame">
                      <Table className="max-md:block max-md:[&_thead]:sr-only max-md:[&_tbody]:block max-md:[&_tr]:grid max-md:[&_tr]:grid-cols-1 max-md:[&_td]:block">
                        <TableHeader>
                          <TableRow>
                            <TableHead>
                              {discloseSources ? 'Version / source' : 'Version'}
                            </TableHead>
                            <TableHead>Download</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {releases.data.items?.map((release) => (
                            <TableRow key={release.occurrence_id}>
                              <TableCell className="max-w-xl align-top whitespace-normal">
                                <p className="break-words font-medium">{release.title}</p>
                                <p className="help mt-1">
                                  {release.provider &&
                                    `${sourceName(release.provider, release.provider_name)} · `}
                                  {release.size > 0 ? bytes(release.size) : 'Size unknown'}
                                  {release.languages.length > 0 &&
                                    ` · ${release.languages.join(', ')}`}
                                </p>
                                {release.published_at && (
                                  <p className="help">{date(release.published_at)}</p>
                                )}
                              </TableCell>
                              <TableCell className="align-top whitespace-normal">
                                {release.services.length ? (
                                  <div className="flex flex-wrap gap-5">
                                    {release.services.map((service) => (
                                      <DownloadAction
                                        key={`${visitor?.scope ?? 'admin'}:${id}:${selection.season}:${selection.episode}:${release.occurrence_id}:${release.fingerprint}:${service.service}`}
                                        workID={id}
                                        release={release}
                                        service={service}
                                        selection={selection}
                                      />
                                    ))}
                                  </div>
                                ) : (
                                  <p className="help">
                                    {visitor ? (
                                      'No service is available for this version.'
                                    ) : (
                                      <>
                                        <Link to="/debrid/settings">
                                          Configure a debrid service
                                        </Link>{' '}
                                        to download this version.
                                      </>
                                    )}
                                  </p>
                                )}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  ) : (
                    <Empty
                      title={cachedOnly ? 'No cached releases' : 'No source releases'}
                      description={
                        cachedOnly
                          ? 'No cached versions match these filters. Turn off Cached results only to see other permitted versions.'
                          : visitor
                            ? 'No available versions match these filters. Private sources require verified cached files or your own permitted announce URL.'
                            : title.data.type === 'tv'
                              ? 'No releases match this episode and the selected filters. Try another episode or disable the language filter.'
                              : 'No releases match the selected source and language filters. Adjust them to see other versions.'
                      }
                    />
                  )}
                  <Pager
                    total={releases.data.total}
                    limit={releases.data.limit}
                    offset={releases.data.offset}
                    onChange={pagination.setOffset}
                    busy={releases.isFetching}
                  />
                </>
              )
            )}
          </>
        )
      )}
    </div>
  )
}
