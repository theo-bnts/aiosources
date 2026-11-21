import { InfoTip } from '@/components/ui/info-tip'
import {
  MagnifyingGlassIcon,
  ListChecksIcon,
  ArrowsClockwiseIcon,
  CheckIcon,
} from '@phosphor-icons/react'
import { useEffect, useRef, useState } from 'react'
import { FilterSelect } from '@/components/filter-select'
import { reviewMediaTypes } from '@/lib/connection-options'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, ApiError, params, queryClient, refreshData } from '@/lib/api'
import { browserLanguage } from '@/lib/language-preferences'
import { useSourceNames } from '@/lib/source-names'
import type { MatchDiagnostic, ReviewDetail, ReviewWork } from '@/lib/aiosource-types'
import type { Items } from '@/lib/types'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Disclosure } from '@/components/ui/disclosure'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { ConfirmAction, Empty, ErrorNotice, Loading, ResultCount } from '@/components/common'

function formatWorkTitle(work: ReviewWork) {
  return (
    work.display_title ||
    work.titles[0] ||
    (work.type === 'book'
      ? work.openlibrary || 'Untitled book'
      : work.type === 'game'
        ? work.igdb
          ? `IGDB ${work.igdb}`
          : 'Untitled game'
        : work.type === 'software'
          ? work.software_key || 'Untitled software'
          : work.type === 'music'
            ? work.musicbrainz
              ? `MusicBrainz ${work.musicbrainz}`
              : 'Untitled release'
            : work.tmdb
              ? `TMDB ${work.tmdb}`
              : 'Untitled work')
  )
}

function WorkSummary({ work }: { work: ReviewWork }) {
  const title = formatWorkTitle(work)
  const aliases = work.titles.filter((alias) => alias !== title).slice(0, 2)
  return (
    <div className="min-w-0 space-y-1">
      <p className="break-words font-medium">{title}</p>
      <p className="help">
        {work.type === 'movie'
          ? 'Movie'
          : work.type === 'book'
            ? 'Book'
            : work.type === 'game'
              ? 'Game'
              : work.type === 'software'
                ? work.software?.part === 'o'
                  ? 'Operating System'
                  : 'Software'
                : work.type === 'music'
                  ? work.music?.primary_type || 'Music'
                  : 'Series'}{' '}
        ·{' '}
        {work.type === 'software'
          ? work.software?.vendor || 'Vendor unknown'
          : work.years.join(', ') || 'Year unknown'}
      </p>
      <p className="mono break-words text-xs">
        {work.type === 'game' ? (
          `IGDB ${work.igdb} · ${work.game?.platforms?.map((p) => p.name).join(', ') || 'Platform unknown'}`
        ) : work.type === 'software' ? (
          [
            work.software?.winget_id && `WinGet ${work.software.winget_id}`,
            work.software?.homebrew_cask && `Homebrew ${work.software.homebrew_cask}`,
            work.software?.nvd_cpe,
            work.software?.platforms?.length ? work.software.platforms.join(', ') : null,
          ]
            .filter(Boolean)
            .join(' · ') || work.software_key
        ) : work.type === 'book' ? (
          [work.book?.authors?.join(', '), work.openlibrary].filter(Boolean).join(' · ')
        ) : work.type === 'music' ? (
          [
            work.music?.artists?.join(', '),
            work.musicbrainz && `MBID ${work.musicbrainz}`,
            work.music?.discogs_ids?.[0] && `Discogs ${work.music.discogs_ids[0]}`,
            work.music?.wikidata_id,
          ]
            .filter(Boolean)
            .join(' · ')
        ) : (
          <>
            {work.imdb && `${work.imdb} · `}TMDB {work.tmdb}
          </>
        )}
        {work.tvdb && ` · TVDB ${work.tvdb}`}
      </p>
      {aliases.length > 0 && (
        <p className="help break-words">Also known as: {aliases.join(' · ')}</p>
      )}
    </div>
  )
}

export function IdentificationReview({
  item,
  onClose,
}: {
  item: MatchDiagnostic
  onClose: () => void
}) {
  const language = browserLanguage()
  const [reviewVersion, setReviewVersion] = useState(0)
  const [busy, setBusy] = useState(false)
  const detail = useQuery({
    queryKey: ['identification-review', item.occurrence_id, language],
    queryFn: ({ signal }) =>
      api<ReviewDetail>(
        `/aiosource/matches/${encodeURIComponent(item.occurrence_id)}?${params({ language })}`,
        { signal },
      ),
    refetchInterval: false,
    refetchOnWindowFocus: false,
    staleTime: 0,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose()
      }}
    >
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>Review identification</DialogTitle>
          <DialogDescription className="break-words">
            {item.title || item.source_id}
          </DialogDescription>
        </DialogHeader>
        <ErrorNotice error={detail.error} retry={() => void detail.refetch()} />
        {detail.isPending ? (
          <Loading />
        ) : (
          detail.data && (
            <ReviewForm
              key={`${item.occurrence_id}:${detail.data.record.fingerprint}:${detail.data.review?.revision ?? 0}:${reviewVersion}`}
              detail={detail.data}
              occurrence={item.occurrence_id}
              onClose={onClose}
              onBusyChange={setBusy}
              reload={async () => {
                // Load candidates before a changed record can replace the edited form.
                await queryClient.invalidateQueries(
                  { queryKey: ['review-candidates'] },
                  { throwOnError: true },
                )
                await detail.refetch({ throwOnError: true })
                setReviewVersion((version) => version + 1)
              }}
            />
          )
        )}
      </DialogContent>
    </Dialog>
  )
}

function ReviewForm({
  detail,
  occurrence,
  onClose,
  onBusyChange,
  reload,
}: {
  detail: ReviewDetail
  occurrence: string
  onClose: () => void
  onBusyChange: (busy: boolean) => void
  reload: () => Promise<void>
}) {
  const sourceName = useSourceNames()
  const formRef = useRef<HTMLFormElement>(null)
  const [selected, setSelected] = useState<ReviewWork | null>(null)
  const [q, setQuery] = useState(detail.suggested_query)
  const [kind, setKind] = useState('')
  const [year, setYear] = useState(String(detail.suggested_year ?? ''))
  const language = browserLanguage()
  const [override, setOverride] = useState(
    (detail.review?.active && detail.review.override_episode) || false,
  )
  const [season, setSeason] = useState(String(detail.review?.season ?? detail.record.season ?? ''))
  const [episode, setEpisode] = useState(
    String(detail.review?.episode ?? detail.record.episode ?? ''),
  )
  const canSearch = q.trim().length >= 3 || /^\d+$/.test(q.trim())
  const candidates = useQuery({
    queryKey: ['review-candidates', q, kind, year, language],
    queryFn: ({ signal }) =>
      api<Items<ReviewWork>>(`/aiosource/candidates?${params({ q, type: kind, year, language })}`, {
        signal,
      }),
    enabled: canSearch && (!year || /^\d{4}$/.test(year)),
    refetchInterval: false,
    staleTime: 30_000,
  })
  const save = useMutation({
    mutationFn: (automatic: boolean) =>
      api(`/aiosource/matches/${encodeURIComponent(occurrence)}`, {
        method: 'PUT',
        body: JSON.stringify({
          fingerprint: detail.record.fingerprint,
          revision: detail.review?.revision ?? 0,
          automatic,
          ...(!automatic && selected
            ? {
                work_id: selected.id,
                work_revision: selected.revision,
                override_episode: selected.type === 'tv' && override,
                ...(selected.type === 'tv' && override
                  ? {
                      season: season === '' ? null : Number(season),
                      episode: episode === '' ? null : Number(episode),
                    }
                  : {}),
              }
            : {}),
        }),
      }),
    onSuccess: async () => {
      markSaved()
      onClose()
      await refreshData()
    },
  })
  const markSaved = useUnsavedChanges(Boolean(selected), 'identification review', save.isPending)
  useEffect(() => {
    onBusyChange(save.isPending)
    return () => onBusyChange(false)
  }, [onBusyChange, save.isPending])
  const readDraftQuery = () =>
    formRef.current ? String(new FormData(formRef.current).get('q') ?? '').trim() : q
  const invalidEpisode =
    override &&
    selected?.type === 'tv' &&
    (!/^\d{0,3}$/.test(season) ||
      !/^\d{0,5}$/.test(episode) ||
      (episode !== '' && (season === '' || Number(episode) < 1)))
  return (
    <>
      <p className="help break-words">
        {sourceName(detail.record.provider)} · {detail.record.source_id}
      </p>
      <div className="grid gap-4 sm:grid-cols-2">
        <section className="space-y-3 rounded-md border p-4">
          <div className="flex min-w-0 items-center gap-1.5">
            <h3 className="font-medium">Current identification</h3>
            <InfoTip label="Current identification">
              Automatic updates retain manual choices. Changed title, hash, identifiers or numbering
              requires another review.
            </InfoTip>
          </div>
          {detail.work ? (
            <WorkSummary work={detail.work} />
          ) : (
            <p className="help">No current title selected.</p>
          )}
          <Badge variant={detail.review?.active ? 'info' : 'secondary'}>
            {detail.review?.active ? 'Reviewed' : 'Automatic'}
          </Badge>
          {detail.review?.active && detail.review.source_changed && (
            <p className="notice notice-warning">
              The source changed after this review. Choose and confirm a current identification.
            </p>
          )}
        </section>
        <section className="space-y-3 rounded-md border p-4">
          <div className="flex min-w-0 items-center gap-1.5">
            <h3 className="font-medium">Source identifiers</h3>
            <InfoTip label="Source identifiers">
              A review corrects this occurrence without rewriting the source data.
            </InfoTip>
          </div>
          <p className="mono break-words text-xs">
            {[
              detail.record.igdb && `IGDB ${detail.record.igdb}`,
              detail.record.software_key && `Software ${detail.record.software_key}`,
              detail.software_evidence?.winget_id && `WinGet ${detail.software_evidence.winget_id}`,
              detail.software_evidence?.homebrew_cask &&
                `Homebrew ${detail.software_evidence.homebrew_cask}`,
              detail.software_evidence?.nvd_cpe,
              detail.software_evidence?.vendor && `Vendor ${detail.software_evidence.vendor}`,
              detail.software_evidence?.version && `Version ${detail.software_evidence.version}`,
              detail.record.musicbrainz && `MBID ${detail.record.musicbrainz}`,
              detail.record.imdb,
              detail.record.tmdb && `TMDB ${detail.record.tmdb}`,
              detail.record.tvdb && `TVDB ${detail.record.tvdb}`,
              detail.book_evidence?.isbn && `ISBN ${detail.book_evidence.isbn}`,
              detail.book_evidence?.openlibrary,
              detail.music_evidence?.mbid &&
                detail.music_evidence.mbid !== detail.record.musicbrainz &&
                `MBID ${detail.music_evidence.mbid}`,
              detail.music_evidence?.discogs_id && `Discogs ${detail.music_evidence.discogs_id}`,
              detail.music_evidence?.wikidata_id && `Wikidata ${detail.music_evidence.wikidata_id}`,
              detail.music_evidence?.barcode && `Barcode ${detail.music_evidence.barcode}`,
              detail.music_evidence?.catalog_number &&
                `CatNo ${detail.music_evidence.catalog_number}`,
              detail.music_evidence?.artist && `Artist ${detail.music_evidence.artist}`,
            ]
              .filter(Boolean)
              .join(' · ') || 'None supplied'}
          </p>
        </section>
      </div>
      <form
        ref={formRef}
        className="toolbar"
        onSubmit={(event) => {
          event.preventDefault()
          setQuery(String(new FormData(event.currentTarget).get('q') ?? '').trim())
        }}
      >
        <div className="field min-w-0 basis-full sm:basis-auto sm:flex-1">
          <Label htmlFor="review-query">Title, author or identifier</Label>
          <Input
            id="review-query"
            name="q"
            defaultValue={q}
            placeholder="At least 3 characters, or an identifier"
          />
        </div>
        <div className="field basis-full sm:basis-auto">
          <Label htmlFor="review-kind">Media type</Label>
          <FilterSelect
            id="review-kind"
            label="Media type"
            allLabel="All media"
            value={kind}
            onChange={(nextKind) => {
              setQuery(readDraftQuery())
              setKind(nextKind)
            }}
            options={reviewMediaTypes}
            selectGroups
          />
        </div>
        <div className="field w-28">
          <Label htmlFor="review-year">Year</Label>
          <Input
            id="review-year"
            name="year"
            type="text"
            inputMode="numeric"
            maxLength={4}
            pattern="[0-9]{4}"
            placeholder="Any year"
            value={year}
            onChange={(event) => {
              setQuery(readDraftQuery())
              setYear(event.target.value)
            }}
          />
        </div>
        <Button type="submit" variant="outline" className="self-end">
          <MagnifyingGlassIcon aria-hidden="true" />
          Search
        </Button>
      </form>
      <ErrorNotice error={candidates.error} retry={() => void candidates.refetch()} />
      {candidates.isLoading && <Loading label="Searching local information" />}
      {candidates.data && (
        <ResultCount total={candidates.data.items?.length ?? 0} label="candidates" />
      )}
      <div
        className="max-h-72 space-y-2 overflow-y-auto"
        role="radiogroup"
        aria-label="Candidate titles"
        aria-busy={candidates.isFetching}
      >
        {candidates.data?.items?.map((work) => {
          const title = formatWorkTitle(work)
          return (
            <label
              key={work.id}
              className={`flex cursor-pointer items-start gap-3 rounded-md border p-4 ${selected?.id === work.id ? 'border-primary bg-primary/5' : 'hover:bg-muted/40'}`}
            >
              <input
                type="radio"
                name="review-work"
                className="mt-1 accent-primary"
                checked={selected?.id === work.id}
                onChange={() => setSelected(work)}
                disabled={save.isPending}
                aria-label={`Choose ${title}`}
              />
              <WorkSummary work={work} />
            </label>
          )
        })}
      </div>
      {!canSearch && (
        <p className="help" role="status">
          Enter at least 3 characters or an identifier to search.
        </p>
      )}
      {canSearch && year && !/^\d{4}$/.test(year) && (
        <p className="help" role="status">
          Enter a 4-digit year, or leave year empty.
        </p>
      )}
      {canSearch &&
        candidates.isSuccess &&
        !candidates.isFetching &&
        !candidates.data.items?.length && (
          <Empty
            title="No candidates in this view"
            description="Try another title, author or identifier. Only information available on this instance can be selected."
          />
        )}
      {selected && !candidates.data?.items?.some((work) => work.id === selected.id) && (
        <section className="space-y-3 rounded-md border border-primary/40 p-4">
          <h3 className="font-medium">Selected identification</h3>
          <WorkSummary work={selected} />
        </section>
      )}

      {selected?.type === 'tv' && (
        <Disclosure
          title="Season & episode correction"
          help={
            <>
              Leave episode empty for a pack, or both empty for unknown numbering. Playback still
              verifies the provider’s file list.
            </>
          }
        >
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="review-episode-override">Override source numbering</Label>
            <Switch
              id="review-episode-override"
              checked={override}
              onCheckedChange={setOverride}
              disabled={save.isPending}
            />
          </div>
          {override && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="field">
                  <Label htmlFor="review-season">Season</Label>
                  <Input
                    id="review-season"
                    inputMode="numeric"
                    disabled={save.isPending}
                    value={season}
                    onChange={(event) => setSeason(event.target.value)}
                    placeholder="0–999"
                  />
                </div>
                <div className="field">
                  <Label htmlFor="review-episode">Episode</Label>
                  <Input
                    id="review-episode"
                    inputMode="numeric"
                    disabled={save.isPending}
                    value={episode}
                    onChange={(event) => setEpisode(event.target.value)}
                    placeholder="Empty for a season pack"
                  />
                </div>
              </div>

              {invalidEpisode && (
                <p role="alert" className="help text-destructive">
                  Enter a season from 0 to 999 and an episode from 1 to 99999, or leave them empty.
                </p>
              )}
            </>
          )}
        </Disclosure>
      )}
      <ErrorNotice error={save.error} />
      {save.error instanceof ApiError && save.error.status === 409 && (
        <ConfirmAction
          title="Review the latest data?"
          description="The source, metadata or review changed. Your previous selection will be cleared so you can check the latest information."
          label="Load current information"
          variant="warning"
          trigger={
            <Button variant="warning">
              <ListChecksIcon aria-hidden="true" />
              Review latest data
            </Button>
          }
          action={reload}
        />
      )}
      <DialogFooter>
        <div className="mr-auto">
          {detail.review?.active && (
            <ConfirmAction
              title="Return to automatic identification?"
              description="Remove this manual choice and queue automatic matching for this torrent. Imported source data stays intact."
              label="Use automatic matching"
              variant="warning"
              trigger={
                <Button variant="warning" disabled={save.isPending}>
                  <ArrowsClockwiseIcon aria-hidden="true" />
                  Use automatic matching
                </Button>
              }
              action={() => save.mutateAsync(true)}
            />
          )}
        </div>
        <Button
          disabled={!selected || invalidEpisode || save.isPending}
          onClick={() => save.mutate(false)}
        >
          <CheckIcon aria-hidden="true" />
          {save.isPending ? 'Saving…' : 'Confirm identification'}
        </Button>
      </DialogFooter>
    </>
  )
}
