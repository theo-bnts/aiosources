import { MagnifyingGlassIcon, TelevisionIcon } from '@phosphor-icons/react'
import { CatalogCover } from '@/components/catalog-cover'
import { CatalogPosterRail } from '@/components/catalog-poster-rail'
import { catalogCardSummary, catalogTorrentCount } from '@/lib/catalog-cards'
import { useCatalogSearch } from '@/lib/use-catalog-search'
import { useLayoutEffect, useState } from 'react'
import { CatalogReleaseList } from '@/components/catalog-release-list'
import { FilterSelect } from '@/components/filter-select'
import { catalogMediaTypes } from '@/lib/connection-options'
import { Disclosure } from '@/components/ui/disclosure'
import { ResultSorting, ResponseTimeout } from '@/components/result-controls'
import { readResponseSeconds, readResultSorting } from '@/lib/result-preferences'
import { LanguageFilter } from '@/components/language-filter'
import {
  useLanguagePreferences,
  resolvedLanguage,
  browserLanguage,
} from '@/lib/language-preferences'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'

import { params } from '@/lib/api'
import { isWholeNumberString, number } from '@/lib/format'
import {
  catalogAPI,
  catalogQueryKey,
  catalogSearchPath,
  catalogTrendingPath,
  usePublicSearch,
} from '@/lib/public-search'
import { routes } from '@/lib/routes'
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

const softwarePlatformLabels: Record<string, string> = {
  windows: 'Windows',
  macos: 'macOS',
  linux: 'Linux',
  android: 'Android',
  ios: 'iOS',
}

function TitleDetails({ title, compact = true }: { title: CatalogTitle; compact?: boolean }) {
  return (
    <p className={`help break-words ${compact ? 'line-clamp-3' : ''}`}>
      {title.type === 'movie'
        ? 'Movie'
        : title.type === 'book'
          ? 'Book'
          : title.type === 'game'
            ? 'Game'
            : title.type === 'software'
              ? 'Software'
              : title.type === 'music'
                ? title.music_type || 'Music'
                : 'Series'}
      {title.vendor && ` · ${title.vendor}`}
      {!!title.software_platforms?.length &&
        ` · ${title.software_platforms.map((p) => softwarePlatformLabels[p] ?? p).join(', ')}`}
      {title.latest_version && !compact && ` · v${title.latest_version.replace(/^v/i, '')}`}
      {title.license && !compact && ` · ${title.license}`}
      {!!title.platforms?.length && ` · ${title.platforms.map((p) => p.name).join(', ')}`}
      {!!title.authors?.length && ` · ${title.authors.join(', ')}`}
      {!!title.artists?.length && ` · ${title.artists.join(', ')}`}
      {title.years.length > 0 && ` · ${title.years.join(', ')}`}
      {!!title.labels?.length && !compact && ` · ${title.labels.slice(0, 3).join(', ')}`}
      {title.imdb && ` · ${title.imdb}`}
    </p>
  )
}

export function CatalogSearchPage() {
  const visitor = usePublicSearch()
  const searchPath = catalogSearchPath(visitor)
  const language = browserLanguage()
  const [search, setSearch] = useViewSearchParams()
  const listState = useListViewState()
  const q = search.get('q') || ''
  const [draftQuery, setDraftQuery] = useState(q)
  // Synchronize the shortcut before the next input event. The form keeps its
  // own draft so submitting cannot overwrite text entered immediately after it.
  useLayoutEffect(() => setDraftQuery(q), [q])
  const kind = search.get('type') || ''
  const year = search.get('year') || ''
  const query = useCatalogSearch({
    q,
    type: kind,
    year,
    language,
  })
  const visibleSections = query.sections.filter(
    (section) => section.isPending || section.items.length > 0,
  )
  const hasMatches = query.sections.some((section) => section.items.length > 0)
  const resetScrollKey = `${q}\0${kind}\0${year}`
  return (
    <div className={visitor ? 'min-w-0 space-y-6' : 'page min-w-0'}>
      <form
        key={`${q}:${kind}:${year}`}
        className="space-y-4 rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          const data = new FormData(event.currentTarget)
          const next = new URLSearchParams()
          const trimmed = String(data.get('q') || '').trim()
          setDraftQuery(trimmed)
          next.set('q', trimmed)
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
            onInput={(event) => setDraftQuery(event.currentTarget.value)}
            minLength={2}
            maxLength={500}
            required
            placeholder="Search movies, series, games, software, music and books"
          />
        </div>
        <Disclosure title="Filters">
          <div className="grid items-start gap-4 sm:grid-cols-2">
            <div className="field">
              <Label htmlFor="catalog-type">Type</Label>
              <FilterSelect
                id="catalog-type"
                name="type"
                label="Type"
                allLabel="All media"
                defaultValue={kind}
                options={catalogMediaTypes}
                selectGroups
              />
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
            <MagnifyingGlassIcon aria-hidden="true" />
            Search
          </Button>
          <ClearFiltersButton
            onClear={() => {
              setDraftQuery('')
              setSearch(new URLSearchParams(), { flushSync: true })
            }}
          />
        </div>
      </form>
      <ErrorNotice error={query.error} retry={query.refetch} />
      {q.trim().length < 2 ? (
        <Empty
          title="Find a title"
          description="Search your identified catalog, then choose a version to download through your debrid services."
          icon={MagnifyingGlassIcon}
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <ResultCount total={query.total} label="titles" />
            <p
              className="help"
              role="status"
              aria-label="Category search progress"
              data-search-complete={query.complete}
              title={query.categoryStatus}
            >
              · {query.completedCategories}/{query.totalCategories}{' '}
              {query.totalCategories === 1 ? 'category' : 'categories'} complete
            </p>
          </div>
          {visibleSections.length > 0 ? (
            <div
              className="min-w-0 space-y-8"
              aria-label={!hasMatches && query.isPending ? 'Searching titles' : undefined}
            >
              {visibleSections.map((section, index) => (
                <CatalogPosterRail
                  key={section.id}
                  category={{ id: section.id, label: section.label, frame: section.frame }}
                  items={section.items.map((title) => ({
                    id: title.id,
                    title: title.title,
                    cover: title.cover,
                    summary: catalogCardSummary(title),
                    metric: catalogTorrentCount(title.releases),
                    to: `${searchPath}/${title.id}?${params({ q })}`,
                  }))}
                  loading={section.isPending}
                  loadingLabel={`Searching ${section.label.toLowerCase()}`}
                  countLabel={
                    section.items.length < section.total
                      ? `${number(section.items.length)} of ${number(section.total)}`
                      : number(section.total)
                  }
                  eagerRow={index === 0}
                  listState={listState}
                  resetScrollKey={resetScrollKey}
                  dimmed={section.isPlaceholderData && section.isFetching}
                  hasMore={section.hasMore}
                  loadingMore={section.isPlaceholderData && section.isFetching}
                  onLoadMore={section.loadMore}
                />
              ))}
            </div>
          ) : !query.error ? (
            <Empty
              title="No matching titles"
              description={
                visitor
                  ? 'No identified titles match this search.'
                  : 'Only titles with currently identified source releases appear here. Check Sources and Metadata if an imported title is missing.'
              }
            />
          ) : null}
        </>
      )}
      <div className="flex flex-wrap justify-center gap-2">
        <Button asChild variant="outline">
          <Link
            to={`${searchPath}/torrents${draftQuery.trim() ? `?${params({ q: draftQuery.trim() })}` : ''}`}
            state={listState}
          >
            <MagnifyingGlassIcon aria-hidden="true" />
            Search torrents directly
          </Link>
        </Button>
      </div>
    </div>
  )
}

export function CatalogTitlePage({ publicID }: { publicID?: string }) {
  const visitor = usePublicSearch()
  const searchPath = catalogSearchPath(visitor)
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
  const title = useQuery({
    queryKey: catalogQueryKey(visitor, 'title', id, titleLanguage, search.get('q')),
    queryFn: ({ signal }) =>
      catalogAPI<CatalogTitle>(
        `/catalog/${id}?${params({ language: titleLanguage, q: search.get('q') || '' })}`,
        visitor,
        { signal },
      ),
  })
  const parsePositiveInt = (raw: string | null, fallback: number, min = 0) =>
    isWholeNumberString(raw, min) ? Number(raw) : fallback
  const selection: CatalogSelection =
    title.data?.type === 'tv'
      ? {
          season: parsePositiveInt(search.get('season'), title.data.selection.season ?? 1, 0),
          episode: parsePositiveInt(search.get('episode'), title.data.selection.episode ?? 1, 1),
        }
      : {}
  const discloseSources = !visitor || Boolean(visitor.info.sources)
  const sort =
    !discloseSources && search.get('sort') === 'source'
      ? 'size_desc'
      : search.get('sort') || 'size_desc'
  const sorting = (
    search.has('sorting')
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
  ).filter((rule) => discloseSources || rule.field !== 'source')
  const responseSeconds = readResponseSeconds(search.get('response_seconds'))
  const cachedOnly = search.get('cached_only') === 'true'
  const provider = discloseSources ? search.get('provider') || '' : ''
  const query = params({
    ...selection,
    language,
    language_mode: preferences.mode,
    sort,
    sorting: search.has('sorting') && sorting.length ? JSON.stringify(sorting) : '',
    response_seconds: responseSeconds,
    provider,
    cached_only: String(cachedOnly),
    limit: pagination.limit,
    offset: pagination.offset,
  })
  const releases = useQuery({
    queryKey: catalogQueryKey(visitor, 'releases', id, query),
    queryFn: ({ signal }) =>
      catalogAPI<Page<CatalogRelease>>(`/catalog/${id}/releases?${query}`, visitor, { signal }),
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
  const backFallback =
    visitor && !visitor.info.search_enabled && visitor.info.trending_enabled
      ? { to: catalogTrendingPath(visitor), label: 'Trending' }
      : { to: searchPath, label: 'Search' }
  return (
    <div className={visitor ? 'space-y-6' : 'page'}>
      <PageHeader title={title.data?.title || 'Source releases'} back={backFallback} />
      <ErrorNotice error={title.error} retry={() => void title.refetch()} />
      {title.isPending ? (
        <Loading label="Loading title" />
      ) : (
        title.data && (
          <>
            <div className="flex min-w-0 items-start gap-4">
              <CatalogCover
                src={title.data.cover}
                book={title.data.type === 'book'}
                game={title.data.type === 'game'}
                software={title.data.type === 'software'}
                music={title.data.type === 'music'}
                large
              />
              <div className="min-w-0 space-y-2">
                <TitleDetails title={title.data} compact={false} />
                {title.data.type === 'software' && title.data.description && (
                  <p className="text-sm text-muted-foreground">{title.data.description}</p>
                )}
                {title.data.type === 'software' &&
                  (title.data.winget_id || title.data.homebrew_cask || title.data.nvd_cpe) && (
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      {title.data.winget_id &&
                        /^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}\.[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/.test(
                          title.data.winget_id,
                        ) &&
                        !title.data.winget_id.includes('..') && (
                          <a
                            className="data-link text-sm"
                            href={`https://github.com/microsoft/winget-pkgs/tree/master/manifests/${title.data.winget_id[0].toLowerCase()}/${title.data.winget_id.split('.').map(encodeURIComponent).join('/')}`}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            View on WinGet
                          </a>
                        )}
                      {title.data.homebrew_cask &&
                        /^[a-z0-9][a-z0-9@+._-]{0,127}$/.test(title.data.homebrew_cask) &&
                        !title.data.homebrew_cask.includes('..') && (
                          <a
                            className="data-link text-sm"
                            href={`https://formulae.brew.sh/cask/${encodeURIComponent(title.data.homebrew_cask)}`}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            View on Homebrew
                          </a>
                        )}
                      {title.data.nvd_cpe &&
                        /^cpe:2\.3:[ao]:[a-z0-9][a-z0-9._+-]{0,127}:[a-z0-9][a-z0-9._+-]{0,127}$/.test(
                          title.data.nvd_cpe,
                        ) && (
                          <a
                            className="data-link text-sm"
                            href={`https://nvd.nist.gov/products/cpe/search/results?namingFormat=2.3&keyword=${encodeURIComponent(title.data.nvd_cpe)}`}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            View on NVD CPE
                          </a>
                        )}
                    </div>
                  )}
                {title.data.openlibrary && /^OL[1-9][0-9]*[MW]$/.test(title.data.openlibrary) && (
                  <a
                    className="data-link text-sm"
                    href={`https://openlibrary.org/${title.data.openlibrary.endsWith('M') ? 'books' : 'works'}/${title.data.openlibrary}`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    View on Open Library
                  </a>
                )}
                {title.data.musicbrainz &&
                  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(
                    title.data.musicbrainz,
                  ) && (
                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                      <a
                        className="data-link text-sm"
                        href={`https://musicbrainz.org/release-group/${title.data.musicbrainz}`}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        View on MusicBrainz
                      </a>
                      {title.data.discogs_ids?.[0] &&
                        /^(?:master|release):[1-9][0-9]*$/.test(title.data.discogs_ids[0]) && (
                          <a
                            className="data-link text-sm"
                            href={`https://www.discogs.com/${title.data.discogs_ids[0].replace(':', '/')}`}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            View on Discogs
                          </a>
                        )}
                      {title.data.wikidata_id && /^Q[1-9][0-9]*$/.test(title.data.wikidata_id) && (
                        <a
                          className="data-link text-sm"
                          href={`https://www.wikidata.org/wiki/${title.data.wikidata_id}`}
                          target="_blank"
                          rel="noopener noreferrer"
                        >
                          View on Wikidata
                        </a>
                      )}
                    </div>
                  )}
              </div>
            </div>
            <section aria-label="Source release filters" className="space-y-4">
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
                    <TelevisionIcon aria-hidden="true" />
                    Show episode
                  </Button>
                </form>
              )}

              <Disclosure title="Filters">
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
                    if (search.has('offset')) filter('offset', '')
                  }}
                />
                <div className="flex flex-wrap items-end gap-3">
                  {discloseSources && (
                    <div className="field">
                      <Label htmlFor="release-source">Source</Label>
                      <FilterSelect
                        id="release-source"
                        label="Source"
                        allLabel="All sources"
                        value={provider}
                        onChange={(value) => filter('provider', value)}
                        options={sourceOptions.map((value) => ({
                          value,
                          label: sourceName(value),
                        }))}
                      />
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
                        your announce URL in{' '}
                        <Link to={routes.debrid.settings}>Debrid settings</Link> to enable uncached
                        downloads.
                      </p>
                    )}
                  {releases.data.items?.length ? (
                    <CatalogReleaseList
                      items={releases.data.items}
                      id={id}
                      selection={selection}
                      discloseSources={discloseSources}
                    />
                  ) : (
                    <Empty
                      title={
                        releases.data.total > 0
                          ? 'No releases on this page'
                          : cachedOnly
                            ? 'No cached releases'
                            : 'No source releases'
                      }
                      description={
                        releases.data.total > 0
                          ? 'Use the pagination controls to return to the available releases.'
                          : cachedOnly
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
