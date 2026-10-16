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
import { useSourceNames } from '@/lib/source-names'
import { useListViewState, useViewSearchParams } from '@/lib/detail-navigation'
import { useURLPagination } from '@/lib/pagination'
import type { Page } from '@/lib/types'
import type { CatalogTitle, CatalogRelease, CatalogSelection } from '@/lib/catalog-search-types'
import { Empty, ErrorNotice, Loading, PageHeader, Pager, ResultCount } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { DownloadAction } from '@/components/catalog-download'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'

function TitleDetails({ title }: { title: CatalogTitle }) {
  return (
    <p className="help">
      {title.type === 'movie' ? 'Film' : 'Series'}
      {title.years.length > 0 && ` · ${title.years.join(', ')}`}
      {title.imdb && ` · ${title.imdb}`}
    </p>
  )
}

export function CatalogSearchPage() {
  const language = browserLanguage()
  const [search, setSearch] = useViewSearchParams()
  const listState = useListViewState()
  const pagination = useURLPagination()
  const q = search.get('q') || ''
  const kind = search.get('type') || ''
  const year = search.get('year') || ''
  const query = useQuery({
    queryKey: ['catalog-search', q, kind, year, language, pagination.limit, pagination.offset],
    queryFn: ({ signal }) =>
      api<Page<CatalogTitle>>(
        `/catalog/search?${params({ q, type: kind, year, language, limit: pagination.limit, offset: pagination.offset })}`,
        { signal },
      ),
    enabled: q.trim().length >= 2,
  })
  return (
    <div className="page">
      <form
        key={`${q}:${kind}:${year}`}
        className="flex flex-wrap items-end gap-3 rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          const data = new FormData(event.currentTarget)
          const next = new URLSearchParams()
          next.set('q', String(data.get('q') || '').trim())
          if (data.get('type')) next.set('type', String(data.get('type')))
          if (data.get('year')) next.set('year', String(data.get('year')))
          setSearch(next)
        }}
      >
        <div className="field min-w-48 flex-1">
          <Label htmlFor="catalog-query">Title or IMDb / TMDB ID</Label>
          <Input
            id="catalog-query"
            name="q"
            type="search"
            defaultValue={q}
            minLength={2}
            maxLength={500}
            required
            placeholder="Search films and series"
          />
        </div>
        <div className="field">
          <Label htmlFor="catalog-type">Type</Label>
          <select id="catalog-type" name="type" className="native-select" defaultValue={kind}>
            <option value="">Films and series</option>
            <option value="movie">Films</option>
            <option value="tv">Series</option>
          </select>
        </div>
        <div className="field w-28">
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
        <Button type="submit">
          <MagnifyingGlassIcon />
          Search
        </Button>
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
                          <Link
                            to={`/search/${title.id}?${params({ q })}`}
                            state={listState}
                            className="font-medium break-words"
                          >
                            {title.title}
                          </Link>
                          <TitleDetails title={title} />
                        </TableCell>
                        <TableCell className="text-right mono">{number(title.releases)}</TableCell>
                        <TableCell>
                          <Button asChild size="icon-sm" variant="ghost">
                            <Link
                              to={`/search/${title.id}?${params({ q })}`}
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
                description="Only titles with currently identified source releases appear here. Check Sources and Metadata if an imported title is missing."
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

export function CatalogTitlePage() {
  const [preferences, setPreferences] = useLanguagePreferences()
  const language = resolvedLanguage(preferences)
  const titleLanguage = browserLanguage()
  const { id = '' } = useParams()
  const [search, setSearch] = useViewSearchParams()
  const pagination = useURLPagination()
  const sourceName = useSourceNames()
  const date = useDate()
  const title = useQuery({
    queryKey: ['catalog-title', id, titleLanguage, search.get('q')],
    queryFn: ({ signal }) =>
      api<CatalogTitle>(
        `/catalog/${id}?${params({ language: titleLanguage, q: search.get('q') || '' })}`,
        { signal },
      ),
  })
  const selection: CatalogSelection =
    title.data?.type === 'tv'
      ? {
          season: Number(search.get('season') ?? title.data.selection.season ?? 1),
          episode: Number(search.get('episode') ?? title.data.selection.episode ?? 1),
        }
      : {}
  const sort = search.get('sort') || 'size_desc'
  const provider = search.get('provider') || ''
  const query = params({
    ...selection,
    language,
    language_mode: preferences.mode,
    sort,
    provider,
    limit: pagination.limit,
    offset: pagination.offset,
  })
  const releases = useQuery({
    queryKey: ['catalog-releases', id, query],
    queryFn: ({ signal }) =>
      api<Page<CatalogRelease>>(`/catalog/${id}/releases?${query}`, { signal }),
    enabled: Boolean(title.data),
  })
  const sourceOptions = [...(title.data?.providers ?? [])].sort((a, b) =>
    sourceName(a).localeCompare(sourceName(b)),
  )
  if (provider && !sourceOptions.includes(provider)) sourceOptions.push(provider)
  function filter(key: string, value: string) {
    const next = new URLSearchParams(search)
    value ? next.set(key, value) : next.delete(key)
    next.delete('offset')
    setSearch(next)
  }
  return (
    <div className="page">
      <PageHeader
        title={title.data?.title || 'Source releases'}
        back={{ to: '/search', label: 'Search' }}
      />
      <ErrorNotice error={title.error} retry={() => void title.refetch()} />
      {title.isPending ? (
        <Loading label="Loading title" />
      ) : (
        title.data && (
          <>
            <TitleDetails title={title.data} />
            <section
              aria-label="Source release filters"
              className="space-y-4 rounded-md border border-border p-4"
            >
              <LanguageFilter
                value={preferences}
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
                      setSearch(next)
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
                    <Button type="submit" variant="outline">
                      Show episode
                    </Button>
                  </form>
                )}
                <div className="field">
                  <Label htmlFor="release-sort">Sort versions</Label>
                  <select
                    id="release-sort"
                    className="native-select"
                    value={sort}
                    onChange={(event) => filter('sort', event.target.value)}
                  >
                    <option value="recent">Newest first</option>
                    <option value="size_desc">Largest first</option>
                    <option value="source">Source name</option>
                  </select>
                </div>
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
              </div>
            </section>
            <ErrorNotice error={releases.error} retry={() => void releases.refetch()} />
            {releases.isPending ? (
              <Loading label="Checking source releases" />
            ) : (
              releases.data && (
                <>
                  <ResultCount total={releases.data.total} label="source releases" />
                  {releases.data.items?.some((release) =>
                    release.services.some(
                      (service) =>
                        !service.can_add && ['unknown', 'uncached'].includes(service.state),
                    ),
                  ) && (
                    <p className="help">
                      Private sources without an announce URL allow cached downloads only. Add your
                      announce URL in <Link to="/debrid/settings">Debrid settings</Link> to enable
                      uncached downloads.
                    </p>
                  )}
                  {releases.data.items?.length ? (
                    <div className="table-frame">
                      <Table className="max-md:block max-md:[&_thead]:sr-only max-md:[&_tbody]:block max-md:[&_tr]:grid max-md:[&_tr]:grid-cols-1 max-md:[&_td]:block">
                        <TableHeader>
                          <TableRow>
                            <TableHead>Version / source</TableHead>
                            <TableHead>Download</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {releases.data.items?.map((release) => (
                            <TableRow key={release.occurrence_id}>
                              <TableCell className="max-w-xl align-top whitespace-normal">
                                <p className="break-words font-medium">{release.title}</p>
                                <p className="help mt-1">
                                  {sourceName(release.provider)} ·{' '}
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
                                        key={`${id}:${selection.season}:${selection.episode}:${release.occurrence_id}:${release.fingerprint}:${service.service}`}
                                        workID={id}
                                        release={release}
                                        service={service}
                                        selection={selection}
                                      />
                                    ))}
                                  </div>
                                ) : (
                                  <p className="help">
                                    <Link to="/debrid/settings">Configure a debrid service</Link> to
                                    download this version.
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
                      title="No source releases"
                      description={
                        title.data.type === 'tv'
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
