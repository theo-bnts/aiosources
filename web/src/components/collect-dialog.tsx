import { InfoTip } from '@/components/ui/info-tip'
import { PlayIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'

import { api, refreshData } from '@/lib/api'
import type {
  IdentificationStatus,
  Items,
  ProviderDocument,
  ProviderSummary,
  Run,
  RunMode,
  StartRun,
} from '@/lib/types'
import { supportsMetadata } from '@/lib/collection-types'
import { isWholeNumberString, modeLabels } from '@/lib/format'
import {
  buildMetadataFilters,
  formatMetadataFiltersSummary,
  metadataIdentificationOptions,
  parseMetadataCategories,
  suggestedProviderCategories,
} from '@/lib/metadata-filters'
import { useListViewState } from '@/lib/detail-navigation'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { MultiSelect } from '@/components/ui/multi-select'
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { ErrorNotice, Loading } from './common'

export function CollectDialog({
  providerId,
  trigger,
  remote = false,
}: {
  providerId: string
  trigger?: ReactNode
  remote?: boolean
}) {
  const [open, setOpen] = useState(false)
  const provider = providerId
  const [mode, setMode] = useState<RunMode>(remote ? 'incremental' : 'preview')
  const [maxPages, setMaxPages] = useState('')
  const [maxDuration, setMaxDuration] = useState('')
  const [knownPages, setKnownPages] = useState('')
  const [categoriesText, setCategoriesText] = useState('')
  const [identificationStatuses, setIdentificationStatuses] = useState<IdentificationStatus[]>([])
  const navigate = useNavigate()
  const returnView = useListViewState()
  const sources = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
    enabled: open,
  })
  const definition = useQuery({
    queryKey: ['provider', provider],
    queryFn: ({ signal }) =>
      api<ProviderDocument>(`/providers/${encodeURIComponent(provider)}`, { signal }),
    enabled: open && Boolean(provider),
  })
  const metadataSupported = !remote && supportsMetadata(definition.data?.provider)
  const metadataReady = metadataSupported && !definition.isPending && !definition.isError
  const metadataFollowup =
    metadataReady && definition.data?.provider.traversal?.metadata_after_incremental === true
  const modeEligible = mode !== 'metadata' || metadataReady
  const defaultKnownPages = definition.data?.provider.schedule?.known_pages ?? 0
  const definitionReady = Boolean(definition.data) && !definition.isPending && !definition.isError
  const defaultPages = definitionReady
    ? mode === 'preview'
      ? 3
      : (definition.data?.provider.schedule?.max_pages ?? 0)
    : undefined
  const invalidPages = maxPages !== '' && !isWholeNumberString(maxPages, 0, 10000)
  const invalidKnownPages =
    !remote &&
    mode === 'incremental' &&
    knownPages !== '' &&
    !isWholeNumberString(knownPages, 0, 10000)
  const invalidDuration = maxDuration !== '' && !isWholeNumberString(maxDuration, 0, 604800)
  const parsedCategories = parseMetadataCategories(categoriesText)
  const invalidMetadataFilters = !remote && mode === 'metadata' && Boolean(parsedCategories.error)
  const activeMetadataFilters =
    !remote && mode === 'metadata'
      ? buildMetadataFilters(parsedCategories.values, identificationStatuses)
      : undefined
  const activeMetadataSummary = formatMetadataFiltersSummary(activeMetadataFilters)
  const suggestedCategories =
    !remote && mode === 'metadata' ? suggestedProviderCategories(definition.data?.provider) : []
  const mutation = useMutation({
    mutationFn: (request: StartRun) =>
      api<Run>('/runs', { method: 'POST', body: JSON.stringify(request) }),
    onSuccess: async (run) => {
      setOpen(false)
      await refreshData()
      navigate(
        `${run.remote_catalog ? '/remotes/history' : '/runs'}/${encodeURIComponent(run.id)}`,
        {
          state: returnView,
        },
      )
    },
  })
  const items = sources.data?.items?.filter((item) => Boolean(item.remote_catalog) === remote)
  const selected = items?.find((item) => item.id === provider)
  const actions = remote
    ? { preview: 'Preview', incremental: 'Sync changes', full: 'Full resync' }
    : modeLabels
  const eligible = Boolean(
    selected?.valid &&
    selected.enabled &&
    !(
      selected.last_run &&
      (remote ? ['queued', 'running', 'paused'] : ['queued', 'running']).includes(
        selected.last_run.status,
      )
    ),
  )
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!mutation.isPending) {
          setOpen(value)
          mutation.reset()
          if (!value) {
            setMode(remote ? 'incremental' : 'preview')
            setMaxPages('')
            setMaxDuration('')
            setKnownPages('')
            setCategoriesText('')
            setIdentificationStatuses([])
          }
        }
      }}
    >
      <DialogTrigger asChild>
        {trigger ?? (
          <Button>
            <PlayIcon />
            Run manual import
          </Button>
        )}
      </DialogTrigger>
      <DialogContent aria-describedby={undefined} showCloseButton={!mutation.isPending}>
        <DialogHeader>
          <DialogTitle>Run manual import</DialogTitle>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            if (
              eligible &&
              modeEligible &&
              definitionReady &&
              !invalidPages &&
              !invalidDuration &&
              !invalidKnownPages &&
              !invalidMetadataFilters &&
              !mutation.isPending
            )
              mutation.mutate({
                provider_id: provider,
                mode,
                ...(maxPages === '' ? {} : { max_pages: Number(maxPages) }),
                ...(maxDuration === '' ? {} : { max_duration_seconds: Number(maxDuration) }),
                ...(!remote && mode === 'incremental' && knownPages !== ''
                  ? { known_pages: Number(knownPages) }
                  : {}),
                ...(activeMetadataFilters ? { metadata_filters: activeMetadataFilters } : {}),
              })
          }}
        >
          {sources.isPending && (
            <Loading label={remote ? 'Loading remote catalogs' : 'Loading sources'} />
          )}
          <ErrorNotice error={sources.error} retry={() => void sources.refetch()} />
          <ErrorNotice error={definition.error} retry={() => void definition.refetch()} />
          <div className="field">
            <span className="text-sm text-muted-foreground">
              {remote ? 'Remote catalog' : 'Source'}
            </span>
            <p id="collect-provider" className="break-words text-sm font-medium">
              {selected?.name || definition.data?.provider.name || provider}
            </p>
            {selected && !eligible && (
              <p className="help">
                The {remote ? 'remote catalog' : 'source'} must be valid, enabled and have no run in
                progress.
                {remote && selected.last_run?.status === 'paused'
                  ? ' Resume or cancel the paused import in History first.'
                  : ''}
              </p>
            )}
          </div>
          <div className="field">
            <Label
              htmlFor="collect-mode"
              help={
                <>
                  <p>
                    Preview inspects a sample without publishing torrents. Full replaces this
                    source’s published catalog only after successful completion. Incremental imports
                    changes; the first import is Full. Other sources are preserved.
                  </p>
                  {!remote && (
                    <p>
                      Fetching source details requires a JSON source with ID recovery and configured
                      missing-metadata fields. Updated records are queued for local identification
                      automatically.
                    </p>
                  )}
                  {(mode === 'incremental' || mode === 'full') && metadataFollowup && (
                    <p>
                      After successful completion, a separate source-details import will fill
                      configured missing fields only for newly inserted native torrents. Existing
                      torrents encountered by this run are excluded. No eligible new torrents means
                      no follow-up.
                    </p>
                  )}
                </>
              }
            >
              Action
            </Label>
            <select
              id="collect-mode"
              className="native-select"
              value={mode}
              disabled={mutation.isPending}
              onChange={(event) => {
                const next = event.target.value as RunMode
                setMode(next)
                setMaxPages('')
                if (next !== 'metadata') {
                  setCategoriesText('')
                  setIdentificationStatuses([])
                }
              }}
            >
              {Object.entries(actions).map(([value, text]) => (
                <option key={value} value={value} disabled={value === 'metadata' && !metadataReady}>
                  {text}
                </option>
              ))}
            </select>
            <p className="help">
              {mode === 'preview'
                ? 'Inspect a sample without adding torrents to the catalog.'
                : mode === 'full'
                  ? remote
                    ? 'Replace this remote catalog’s local copy after a complete import.'
                    : 'Replace this source’s published catalog after a complete import.'
                  : mode === 'metadata'
                    ? 'Fetch missing source fields for existing torrents.'
                    : remote
                      ? 'Import changes; the first sync downloads the complete catalog.'
                      : 'Import new torrents; the first import is Full.'}
            </p>

            {provider && definition.isPending ? (
              <p className="help" role="status">
                Checking available actions…
              </p>
            ) : !remote && !definition.isError && !metadataSupported ? (
              <p className="help">Fetching source details is unavailable for this source.</p>
            ) : null}
          </div>
          <div className="notice flex flex-wrap items-center gap-1.5" aria-live="polite">
            <p>
              Applied limits:{' '}
              {Number(maxPages || defaultPages) === 0
                ? 'unlimited pages'
                : `${(maxPages || defaultPages) ?? '…'} pages`}{' '}
              · {Number(maxDuration || 0) === 0 ? 'unlimited duration' : `${maxDuration} seconds`}
              {!remote && mode === 'metadata' && activeMetadataSummary.length > 0
                ? ` · ${activeMetadataSummary.join(' · ')}`
                : ''}
              .
            </p>
            <InfoTip label="Applied limits">
              Imports pause when a limit is reached and can resume from the saved point. Limits
              apply only to this run.
            </InfoTip>
          </div>
          {!remote && mode === 'metadata' && (
            <Disclosure
              title={
                <span className="flex flex-wrap items-center justify-between gap-2">
                  <span>Target filters</span>
                  {activeMetadataSummary.length > 0 && (
                    <span className="text-xs font-normal text-muted-foreground">Configured</span>
                  )}
                </span>
              }
            >
              <div className="field">
                <Label
                  htmlFor="collect-metadata-statuses"
                  help="Leave unset to process all torrents missing configured source detail fields. Select Not identified and Needs review to target unresolved releases."
                >
                  Identification status
                </Label>
                <MultiSelect
                  id="collect-metadata-statuses"
                  label="Identification status"
                  allLabel="All identification statuses"
                  value={identificationStatuses}
                  options={metadataIdentificationOptions}
                  disabled={mutation.isPending}
                  onChange={(values) => setIdentificationStatuses(values as IdentificationStatus[])}
                />
              </div>
              <div className="field">
                <Label
                  htmlFor="collect-metadata-categories"
                  help="Optional comma-separated source categories (exact match, ignoring case). Leave empty to include every category."
                >
                  Categories
                </Label>
                <Input
                  id="collect-metadata-categories"
                  value={categoriesText}
                  placeholder="Exact categories, comma-separated (e.g. 2000, 5000)"
                  aria-invalid={invalidMetadataFilters}
                  aria-describedby={invalidMetadataFilters ? 'collect-categories-error' : undefined}
                  disabled={mutation.isPending}
                  onChange={(event) => setCategoriesText(event.target.value)}
                />
                {invalidMetadataFilters && (
                  <p
                    id="collect-categories-error"
                    role="alert"
                    className="text-sm text-destructive"
                  >
                    {parsedCategories.error}
                  </p>
                )}
                {suggestedCategories.length > 0 && (
                  <div className="flex flex-wrap items-center gap-1.5 pt-1">
                    <span className="text-xs text-muted-foreground">Configured on source:</span>
                    {suggestedCategories.map((category) => {
                      const active = parsedCategories.values.some(
                        (item) => item.toLowerCase() === category.toLowerCase(),
                      )
                      return (
                        <Button
                          key={category}
                          type="button"
                          size="sm"
                          variant={active ? 'secondary' : 'outline'}
                          disabled={mutation.isPending}
                          onClick={() => {
                            const next = active
                              ? parsedCategories.values.filter(
                                  (item) => item.toLowerCase() !== category.toLowerCase(),
                                )
                              : [...parsedCategories.values, category]
                            setCategoriesText(next.join(', '))
                          }}
                        >
                          {category}
                        </Button>
                      )
                    })}
                  </div>
                )}
              </div>
            </Disclosure>
          )}
          <Disclosure title="Import limits">
            {!remote && mode === 'incremental' && (
              <div className="field">
                <Label
                  htmlFor="collect-known-pages"
                  help={
                    <>
                      Empty = source schedule default
                      {definition.data
                        ? ` (${defaultKnownPages === 0 ? 'off' : defaultKnownPages})`
                        : ''}
                      . Stop each scope after this many consecutive entirely known pages. 0 disables
                      early stopping. Applies only to this run; the source configuration is
                      unchanged.
                    </>
                  }
                >
                  Known pages (X)
                </Label>
                <Input
                  id="collect-known-pages"
                  type="number"
                  min={0}
                  max={10000}
                  step={1}
                  value={knownPages}
                  placeholder={
                    definition.data ? String(defaultKnownPages) : 'Loading source default…'
                  }
                  aria-invalid={invalidKnownPages}
                  aria-describedby={invalidKnownPages ? 'collect-known-error' : undefined}
                  disabled={mutation.isPending}
                  onChange={(event) => setKnownPages(event.target.value)}
                />

                {invalidKnownPages && (
                  <p id="collect-known-error" role="alert" className="text-sm text-destructive">
                    Enter a whole number from 0 to 10000, or leave empty to use the source default.
                  </p>
                )}
              </div>
            )}
            <div className="field">
              <Label
                htmlFor="collect-budget"
                help={
                  <>
                    Empty = {remote ? 'remote catalog' : 'source'} limit
                    {defaultPages === undefined
                      ? ''
                      : ` (${defaultPages === 0 ? 'unlimited' : `${defaultPages} pages`})`}
                    . 0 explicitly selects unlimited. Imports pause when their budget is reached,
                    never as a false success.
                  </>
                }
              >
                Page budget per attempt
              </Label>
              <Input
                id="collect-budget"
                type="number"
                min={0}
                max={10000}
                step={1}
                value={maxPages}
                placeholder={
                  defaultPages === undefined ? 'Loading source limit…' : String(defaultPages)
                }
                aria-invalid={invalidPages}
                aria-describedby={invalidPages ? 'collect-budget-error' : undefined}
                disabled={mutation.isPending}
                onChange={(event) => setMaxPages(event.target.value)}
              />

              {invalidPages && (
                <p id="collect-budget-error" role="alert" className="text-sm text-destructive">
                  Enter a whole number from 0 to 10000, or leave empty to use the source limit.
                </p>
              )}
            </div>
            <div className="field">
              <Label
                htmlFor="collect-duration"
                help={
                  <>
                    Empty or 0 = unlimited duration. Pauses at a safe response boundary or during a
                    cooldown wait, without interrupting atomic Full publication.
                  </>
                }
              >
                Duration budget per attempt (seconds)
              </Label>
              <Input
                id="collect-duration"
                type="number"
                min={0}
                max={604800}
                step={1}
                value={maxDuration}
                placeholder="0"
                disabled={mutation.isPending}
                aria-invalid={invalidDuration}
                aria-describedby={invalidDuration ? 'collect-duration-error' : undefined}
                onChange={(event) => setMaxDuration(event.target.value)}
              />

              {invalidDuration && (
                <p id="collect-duration-error" role="alert" className="text-sm text-destructive">
                  Enter a whole number from 0 to 604800, or leave empty for unlimited duration.
                </p>
              )}
            </div>
          </Disclosure>
          <ErrorNotice error={mutation.error} />
          <DialogFooter>
            <Button
              type="submit"
              disabled={
                mutation.isPending ||
                !eligible ||
                !modeEligible ||
                !definitionReady ||
                invalidPages ||
                invalidDuration ||
                invalidKnownPages ||
                invalidMetadataFilters
              }
            >
              <PlayIcon aria-hidden="true" />
              {mutation.isPending ? 'Creating…' : 'Run manual import'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
