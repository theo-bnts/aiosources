import {
  ArrowCounterClockwiseIcon,
  EyeIcon,
  LinkIcon,
  MagnifyingGlassIcon,
  XIcon,
} from '@phosphor-icons/react'
import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Disclosure } from '@/components/ui/disclosure'
import { FilterSelect } from '@/components/filter-select'
import { compareListNames } from '@/lib/list-view'
import {
  ClearFiltersButton,
  ConfirmAction,
  ErrorNotice,
  ListEmpty,
  Loading,
  Pager,
  ResultCount,
} from '@/components/common'
import { OccurrenceLink } from '@/lib/occurrence-navigation'
import { api, LIVE_REFRESH_INTERVAL, params, queryClient, refreshData } from '@/lib/api'
import { useSourceNames } from '@/lib/source-names'
import { browserLanguage, languageDisplayName } from '@/lib/language-preferences'
import type {
  CorrespondenceConfidence,
  CorrespondenceRule,
  ReleaseCorrespondenceDetail,
  ReleaseCorrespondenceGroup,
} from '@/lib/aiosource-types'
import type { Items, Page, ProviderSummary } from '@/lib/types'
import { useURLPagination } from '@/lib/pagination'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { bytes, number, useDate } from '@/lib/format'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { InfoTip } from '@/components/ui/info-tip'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

const ruleMeta: Record<
  CorrespondenceRule,
  { label: string; description: string; variant: 'success' | 'info' | 'secondary' }
> = {
  exact_info_hash: {
    label: 'Exact info hash',
    description: 'Identical 40-character BitTorrent SHA-1 info hash across sources.',
    variant: 'success',
  },
  work_exact_size: {
    label: 'Exact byte size',
    description:
      'Same identified title and exact unrounded byte size with compatible video, episode, release group and audio metadata.',
    variant: 'info',
  },
  work_release_name: {
    label: 'Exact release name',
    description:
      'Same identified title, exact canonical release name and shared release group within 1% file size tolerance.',
    variant: 'secondary',
  },
}

const confidenceLabels: Record<CorrespondenceConfidence, string> = {
  exact: 'Exact',
  high: 'High',
  verified: 'Verified',
}

function formatLanguageTag(code: string) {
  if (code.toLowerCase() === 'multi') return 'MULTI'
  return languageDisplayName(code)
}

function LanguageTags({
  audio,
  subtitles,
  prefix = '',
  audioVariant = 'secondary',
  emptyLabel,
}: {
  audio: string[]
  subtitles: string[]
  prefix?: string
  audioVariant?: 'secondary' | 'info' | 'success'
  emptyLabel?: string
}) {
  if (!audio.length && !subtitles.length) {
    return emptyLabel ? <span className="help">{emptyLabel}</span> : null
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      {audio.map((code) => (
        <Badge key={`audio-${code}`} variant={audioVariant}>
          {prefix}
          {formatLanguageTag(code)}
        </Badge>
      ))}
      {subtitles.map((code) => (
        <Badge key={`sub-${code}`} variant="outline">
          {prefix}Sub {formatLanguageTag(code)}
        </Badge>
      ))}
    </div>
  )
}

export function ReleaseCorrespondences() {
  const sourceName = useSourceNames()
  const [inspectingId, setInspectingId] = useState<number | null>(null)
  const [search, setSearch] = useViewSearchParams()
  const rule = search.get('corr_rule') ?? ''
  const provider = search.get('corr_provider') ?? ''
  const enrichedOnly = search.get('corr_enriched') === 'true'
  const q = search.get('corr_q') ?? ''
  const language = browserLanguage()
  const pagination = useURLPagination({ limitKey: 'corr_limit', offsetKey: 'corr_offset' })
  const { limit, offset } = pagination

  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const groups = useQuery({
    queryKey: [
      'release-correspondences',
      { rule, provider, enrichedOnly, q, language, limit, offset },
    ],
    queryFn: ({ signal }) =>
      api<Page<ReleaseCorrespondenceGroup>>(
        `/aiosource/correspondences?${params({
          rule,
          provider,
          enriched: enrichedOnly ? 'true' : '',
          q,
          language,
          limit,
          offset,
        })}`,
        { signal },
      ),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })

  function filter(patch: Record<string, string>) {
    const next = new URLSearchParams(search)
    next.delete('corr_offset')
    for (const [key, value] of Object.entries(patch))
      value ? next.set(key, value) : next.delete(key)
    setSearch(next, { flushSync: true })
  }

  const filtered = Boolean(rule || provider || enrichedOnly || q)

  return (
    <div className="page" id="release-correspondences">
      <form
        key={`${rule}:${provider}:${q}`}
        className="space-y-4 rounded-md border border-border p-4"
        aria-label="Correspondence filters"
        onSubmit={(event) => {
          event.preventDefault()
          const values = new FormData(event.currentTarget)
          filter({
            corr_q: String(values.get('q') ?? '').trim(),
            corr_rule: String(values.get('rule') ?? ''),
            corr_provider: String(values.get('provider') ?? ''),
          })
        }}
      >
        <div className="field min-w-0">
          <Label htmlFor="correspondence-search">
            Title, source ID, info hash or work identifier
          </Label>
          <Input
            id="correspondence-search"
            name="q"
            defaultValue={q}
            placeholder="Search release titles, source IDs, info hashes, IMDb or TMDB IDs"
          />
        </div>
        <Disclosure title="Filters">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="field min-w-0">
              <Label htmlFor="correspondence-rule">Match rule</Label>
              <FilterSelect
                id="correspondence-rule"
                name="rule"
                label="Match rule"
                allLabel="All rules"
                defaultValue={rule}
                options={(
                  Object.entries(ruleMeta) as [CorrespondenceRule, { label: string }][]
                ).map(([value, meta]) => ({ value, label: meta.label }))}
              />
            </div>
            <div className="field min-w-0">
              <Label htmlFor="correspondence-source">Source</Label>
              <FilterSelect
                id="correspondence-source"
                name="provider"
                label="Source"
                allLabel="All sources"
                defaultValue={provider}
                options={[...(providers.data?.items ?? [])]
                  .sort(compareListNames)
                  .map((item) => ({ value: item.id, label: item.name || item.id }))}
              />
            </div>
          </div>
          <div className="flex max-w-sm items-center justify-between gap-3">
            <Label htmlFor="correspondence-enriched">Enriched groups only</Label>
            <Switch
              id="correspondence-enriched"
              checked={enrichedOnly}
              onCheckedChange={(checked) => filter({ corr_enriched: checked ? 'true' : '' })}
            />
          </div>
        </Disclosure>
        <div className="flex flex-wrap gap-2">
          <Button id="correspondence-submit" type="submit">
            <MagnifyingGlassIcon aria-hidden="true" />
            Search
          </Button>
          <ClearFiltersButton
            onClear={() =>
              filter({ corr_rule: '', corr_provider: '', corr_enriched: '', corr_q: '' })
            }
          />
        </div>
      </form>
      <ErrorNotice
        error={groups.error ?? providers.error}
        retry={() => {
          void groups.refetch()
          void providers.refetch()
        }}
      />
      {groups.isPending ? (
        <Loading />
      ) : (
        groups.data && (
          <>
            <ResultCount
              total={groups.data.total}
              label="correspondence groups"
              help="Cross-source release groups link identical releases across sources so verified audio and subtitle languages propagate without modifying source records."
            />
            {groups.data.items?.length ? (
              <>
                <div
                  role="list"
                  aria-label="Cross-source correspondences"
                  className="space-y-3 md:hidden"
                >
                  {groups.data.items.map((item) => {
                    const meta = ruleMeta[item.match_rule]
                    return (
                      <article
                        role="listitem"
                        key={item.id}
                        className="space-y-3 rounded-md border p-4"
                      >
                        <div className="min-w-0 space-y-1">
                          <p className="break-words font-medium">
                            {item.work_title || item.canonical_title || item.match_key}
                          </p>
                          {item.work_title && item.canonical_title && (
                            <p className="help break-words">{item.canonical_title}</p>
                          )}
                          {(item.imdb || item.tmdb) && (
                            <p className="mono text-xs text-muted-foreground">
                              {[item.imdb, item.tmdb ? `TMDB ${item.tmdb}` : '']
                                .filter(Boolean)
                                .join(' · ')}
                            </p>
                          )}
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Badge variant={meta.variant}>{meta.label}</Badge>
                          {item.canonical_size > 0 && (
                            <Badge variant="outline">{bytes(item.canonical_size)}</Badge>
                          )}
                          {item.enriched_count > 0 && (
                            <Badge variant="info">+{number(item.enriched_count)} enriched</Badge>
                          )}
                        </div>
                        <p className="help break-words">
                          {number(item.member_count)} torrents across {number(item.provider_count)}{' '}
                          sources: {item.providers.map((id) => sourceName(id)).join(', ')}
                        </p>
                        <LanguageTags
                          audio={item.propagated_audio}
                          subtitles={item.propagated_subtitles}
                          emptyLabel="No propagated language tags"
                        />
                        <Button
                          variant="outline"
                          onClick={() => setInspectingId(item.id)}
                          aria-label={`Inspect correspondence ${item.work_title || item.canonical_title || item.id}`}
                        >
                          <EyeIcon aria-hidden="true" />
                          Inspect
                        </Button>
                      </article>
                    )
                  })}
                </div>
                <div className="table-frame hidden md:block">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Release / work</TableHead>
                        <TableHead>Rule &amp; size</TableHead>
                        <TableHead>Sources</TableHead>
                        <TableHead>Propagated languages</TableHead>
                        <TableHead>
                          <span className="sr-only">Inspect</span>
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {groups.data.items.map((item) => {
                        const meta = ruleMeta[item.match_rule]
                        return (
                          <TableRow key={item.id}>
                            <TableCell className="min-w-52 max-w-md whitespace-normal break-words">
                              <p className="font-medium">
                                {item.work_title || item.canonical_title || item.match_key}
                              </p>
                              {item.work_title && item.canonical_title && (
                                <p className="help mt-1 break-words">{item.canonical_title}</p>
                              )}
                              {(item.imdb || item.tmdb) && (
                                <p className="mono mt-1 text-xs text-muted-foreground">
                                  {[item.imdb, item.tmdb ? `TMDB ${item.tmdb}` : '']
                                    .filter(Boolean)
                                    .join(' · ')}
                                </p>
                              )}
                            </TableCell>
                            <TableCell className="whitespace-normal">
                              <div className="flex flex-wrap items-center gap-1.5">
                                <Badge variant={meta.variant}>{meta.label}</Badge>
                              </div>
                              <p className="help mt-1">
                                {confidenceLabels[item.confidence]}
                                {item.canonical_size > 0 && ` · ${bytes(item.canonical_size)}`}
                              </p>
                            </TableCell>
                            <TableCell className="min-w-44 max-w-xs whitespace-normal break-words">
                              <p className="text-sm font-medium">
                                {number(item.member_count)} torrents · {number(item.provider_count)}{' '}
                                sources
                              </p>
                              <p className="help mt-1">
                                {item.providers.map((id) => sourceName(id)).join(', ')}
                              </p>
                              {item.enriched_count > 0 && (
                                <Badge variant="info" className="mt-1.5">
                                  +{number(item.enriched_count)} enriched
                                </Badge>
                              )}
                            </TableCell>
                            <TableCell className="min-w-44 max-w-xs whitespace-normal">
                              <LanguageTags
                                audio={item.propagated_audio}
                                subtitles={item.propagated_subtitles}
                                emptyLabel="None"
                              />
                            </TableCell>
                            <TableCell>
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => setInspectingId(item.id)}
                                aria-label={`Inspect correspondence ${item.work_title || item.canonical_title || item.id}`}
                              >
                                <EyeIcon aria-hidden="true" />
                                Inspect
                              </Button>
                            </TableCell>
                          </TableRow>
                        )
                      })}
                    </TableBody>
                  </Table>
                </div>
              </>
            ) : (
              <ListEmpty
                title="No correspondences in this view"
                description="Correspondences appear automatically once multiple sources carry the same verified release."
                filtered={filtered}
                offset={offset}
                onFirstPage={() => pagination.setOffset(0)}
              />
            )}
            <Pager
              total={groups.data.total}
              limit={groups.data.limit}
              offset={offset}
              onChange={pagination.setOffset}
              busy={groups.isFetching}
            />
          </>
        )
      )}
      {inspectingId !== null && (
        <CorrespondenceDetailDialog
          groupId={inspectingId}
          onSelectGroup={(nextId) => setInspectingId(nextId)}
          onClose={() => setInspectingId(null)}
        />
      )}
    </div>
  )
}

function CorrespondenceDetailDialog({
  groupId,
  onSelectGroup,
  onClose,
}: {
  groupId: number
  onSelectGroup: (nextGroupId: number) => void
  onClose: () => void
}) {
  const sourceName = useSourceNames()
  const date = useDate()
  const language = browserLanguage()

  const detail = useQuery({
    queryKey: ['release-correspondence-detail', groupId, language],
    queryFn: ({ signal }) =>
      api<ReleaseCorrespondenceDetail>(
        `/aiosource/correspondences/${groupId}?${params({ language })}`,
        { signal },
      ),
    refetchInterval: false,
    refetchOnWindowFocus: false,
    staleTime: 0,
  })

  async function applyUpdatedGroup(nextGroupId: number) {
    await queryClient.invalidateQueries({ queryKey: ['release-correspondences'] })
    void refreshData()
    if (nextGroupId <= 0) {
      onClose()
      return
    }
    if (nextGroupId !== groupId) {
      onSelectGroup(nextGroupId)
      return
    }
    await detail.refetch()
  }

  const unlink = useMutation({
    mutationFn: (occurrenceId: string) =>
      api<{ group_id: number }>(
        `/aiosource/correspondences/${groupId}/members/${encodeURIComponent(occurrenceId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: async (res) => {
      await applyUpdatedGroup(res.group_id)
    },
  })

  const restore = useMutation({
    mutationFn: (occurrenceId: string) =>
      api<{ group_id: number }>(
        `/aiosource/correspondences/${groupId}/exclusions/${encodeURIComponent(occurrenceId)}/restore`,
        { method: 'POST' },
      ),
    onSuccess: async (res) => {
      await applyUpdatedGroup(res.group_id)
    },
  })

  const busy = unlink.isPending || restore.isPending
  const group = detail.data?.group
  const meta = group ? ruleMeta[group.match_rule] : null

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose()
      }}
    >
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {group
              ? group.work_title || group.canonical_title || `Correspondence #${group.id}`
              : 'Release correspondence'}
          </DialogTitle>
          <DialogDescription>
            {group ? (
              <>
                {group.canonical_title && group.canonical_title !== group.work_title
                  ? `${group.canonical_title} · `
                  : ''}
                {meta?.label} ({confidenceLabels[group.confidence]})
                {group.canonical_size > 0
                  ? ` · ${bytes(group.canonical_size)} (${number(group.canonical_size)} B)`
                  : ''}
              </>
            ) : (
              'Inspect linked releases and propagated audio or subtitle tags.'
            )}
          </DialogDescription>
        </DialogHeader>
        <ErrorNotice
          error={detail.error ?? restore.error}
          retry={detail.error ? () => void detail.refetch() : undefined}
        />
        {detail.isPending ? (
          <Loading label="Loading correspondence details" />
        ) : (
          detail.data &&
          group &&
          meta && (
            <div className="space-y-5">
              <div className="space-y-2 rounded-md border border-border bg-muted/30 p-3.5">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={meta.variant}>
                    <LinkIcon aria-hidden="true" />
                    {meta.label}
                  </Badge>
                  {(group.imdb || group.tmdb) && (
                    <span className="mono text-xs text-muted-foreground">
                      {[group.imdb, group.tmdb ? `TMDB ${group.tmdb}` : '']
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  )}
                  <InfoTip label="Match rule details" size="sm">
                    {meta.description}
                  </InfoTip>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">
                    Shared across {number(group.member_count)} torrents in{' '}
                    {number(group.provider_count)} sources
                    {group.enriched_count > 0
                      ? ` · enriches ${number(group.enriched_count)} ${group.enriched_count === 1 ? 'torrent' : 'torrents'}`
                      : ''}
                  </p>
                  <LanguageTags
                    audio={group.propagated_audio}
                    subtitles={group.propagated_subtitles}
                    emptyLabel="No concrete audio or subtitle tags to propagate in this group."
                  />
                </div>
              </div>

              <section className="space-y-2.5" aria-label="Linked torrents">
                <h3 className="text-sm font-semibold">
                  Linked torrents ({number(detail.data.members.length)})
                </h3>
                <div className="space-y-2.5">
                  {detail.data.members.map((member) => {
                    const memberRule = ruleMeta[member.match_rule]
                    return (
                      <article
                        key={member.occurrence_id}
                        className="space-y-2.5 rounded-md border border-border p-3.5"
                      >
                        <div className="flex flex-wrap items-start justify-between gap-2">
                          <div className="min-w-0 flex-1 space-y-1">
                            <OccurrenceLink
                              className="data-link break-words text-sm font-medium"
                              occurrenceId={member.occurrence_id}
                            >
                              {member.title || member.source_id}
                            </OccurrenceLink>
                            <p className="help break-words">
                              {sourceName(member.provider_id)} · {member.source_id}
                              {member.size > 0 &&
                                ` · ${bytes(member.size)} (${number(member.size)} B)`}
                            </p>
                            {member.hash && (
                              <p className="mono break-all text-xs text-muted-foreground">
                                {member.hash}
                              </p>
                            )}
                          </div>
                          <div className="flex shrink-0 items-center gap-1.5">
                            <Badge variant={memberRule.variant}>{memberRule.label}</Badge>
                            <ConfirmAction
                              trigger={
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="sm"
                                  disabled={busy}
                                  aria-label={`Unlink ${member.title || member.source_id}`}
                                >
                                  <XIcon aria-hidden="true" />
                                  Unlink
                                </Button>
                              }
                              title="Unlink torrent from correspondence?"
                              description={
                                <>
                                  <p>
                                    Exclude{' '}
                                    <span className="font-medium text-foreground">
                                      {member.title || member.source_id}
                                    </span>{' '}
                                    ({sourceName(member.provider_id)}) from this correspondence
                                    group.
                                  </p>
                                  <p className="mt-2">
                                    Its languages will no longer be shared with or inherited from
                                    the other sources. This exclusion persists across recomputations
                                    until restored.
                                  </p>
                                </>
                              }
                              label="Unlink torrent"
                              variant="destructive"
                              action={() => unlink.mutateAsync(member.occurrence_id)}
                            />
                          </div>
                        </div>
                        <div className="grid gap-2 border-t border-border/60 pt-2.5 sm:grid-cols-2">
                          <div className="space-y-1">
                            <p className="text-xs font-medium text-muted-foreground">Native tags</p>
                            <LanguageTags
                              audio={member.own_audio}
                              subtitles={member.own_subtitles}
                              emptyLabel="None detected natively"
                            />
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs font-medium text-muted-foreground">
                              Added from peers
                            </p>
                            <LanguageTags
                              audio={member.contributed_audio}
                              subtitles={member.contributed_subtitles}
                              prefix="+"
                              audioVariant="info"
                              emptyLabel="Already has all shared tags"
                            />
                          </div>
                        </div>
                      </article>
                    )
                  })}
                </div>
              </section>

              {detail.data.exclusions.length > 0 && (
                <section className="space-y-2.5" aria-label="Excluded torrents">
                  <h3 className="text-sm font-semibold">
                    Excluded from this group ({number(detail.data.exclusions.length)})
                  </h3>
                  <div className="space-y-2">
                    {detail.data.exclusions.map((item) => (
                      <div
                        key={item.occurrence_id}
                        className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-dashed border-border p-3"
                      >
                        <div className="min-w-0 flex-1 space-y-0.5">
                          <OccurrenceLink
                            className="data-link break-words text-sm font-medium"
                            occurrenceId={item.occurrence_id}
                          >
                            {item.title || item.source_id}
                          </OccurrenceLink>
                          <p className="help break-words">
                            {sourceName(item.provider_id)} · {item.source_id} · Excluded{' '}
                            {date(item.created_at)}
                          </p>
                        </div>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => restore.mutate(item.occurrence_id)}
                          aria-label={`Restore ${item.title || item.source_id}`}
                        >
                          <ArrowCounterClockwiseIcon aria-hidden="true" />
                          Restore
                        </Button>
                      </div>
                    ))}
                  </div>
                </section>
              )}
            </div>
          )
        )}
      </DialogContent>
    </Dialog>
  )
}
