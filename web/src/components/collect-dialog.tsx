import { useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { PlayIcon } from '@phosphor-icons/react'
import { api, refreshData } from '@/lib/api'
import type { Items, ProviderDocument, ProviderSummary, Run, RunMode, StartRun } from '@/lib/types'
import { supportsMetadata } from '@/lib/collection-types'
import { modeLabels } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { ErrorNotice, Loading } from './common'

export function CollectDialog({
  providerId,
  trigger,
}: {
  providerId?: string
  trigger?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [provider, setProvider] = useState(providerId ?? '')
  const [mode, setMode] = useState<RunMode>('preview')
  const [maxPages, setMaxPages] = useState('')
  const [maxDuration, setMaxDuration] = useState('')
  const [knownPages, setKnownPages] = useState('')
  const navigate = useNavigate()
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
  const metadataSupported = supportsMetadata(definition.data?.provider)
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
  const invalidPages =
    maxPages !== '' &&
    (!/^\d+$/.test(maxPages) || !Number.isSafeInteger(Number(maxPages)) || Number(maxPages) > 10000)
  const invalidKnownPages =
    mode === 'incremental' &&
    knownPages !== '' &&
    (!/^\d+$/.test(knownPages) ||
      !Number.isSafeInteger(Number(knownPages)) ||
      Number(knownPages) > 10000)
  const invalidDuration =
    maxDuration !== '' &&
    (!/^\d+$/.test(maxDuration) ||
      !Number.isSafeInteger(Number(maxDuration)) ||
      Number(maxDuration) > 604800)
  const mutation = useMutation({
    mutationFn: (request: StartRun) =>
      api<Run>('/runs', { method: 'POST', body: JSON.stringify(request) }),
    onSuccess: async (run) => {
      setOpen(false)
      await refreshData()
      navigate(`/runs/${encodeURIComponent(run.id)}`)
    },
  })
  const selected = sources.data?.items?.find((item) => item.id === provider)
  const eligible = Boolean(
    selected?.valid &&
    selected.enabled &&
    !(selected.last_run && ['queued', 'running'].includes(selected.last_run.status)),
  )
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!mutation.isPending) {
          setOpen(value)
          mutation.reset()
        }
      }}
    >
      <DialogTrigger asChild>
        {trigger ?? (
          <Button>
            <PlayIcon />
            Start import
          </Button>
        )}
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Start import</DialogTitle>
          <DialogDescription>
            Choose what to do with this source. Follow progress and resume interrupted imports in
            History.
          </DialogDescription>
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
              !mutation.isPending
            )
              mutation.mutate({
                provider_id: provider,
                mode,
                ...(maxPages === '' ? {} : { max_pages: Number(maxPages) }),
                ...(maxDuration === '' ? {} : { max_duration_seconds: Number(maxDuration) }),
                ...(mode === 'incremental' && knownPages !== ''
                  ? { known_pages: Number(knownPages) }
                  : {}),
              })
          }}
        >
          {sources.isPending && <Loading label="Loading sources" />}
          <ErrorNotice error={sources.error} retry={() => void sources.refetch()} />
          <ErrorNotice error={definition.error} retry={() => void definition.refetch()} />
          <div className="field">
            <Label htmlFor="collect-provider">Source</Label>
            <select
              id="collect-provider"
              className="native-select w-full"
              required
              value={provider}
              onChange={(event) => setProvider(event.target.value)}
              disabled={mutation.isPending}
            >
              <option value="">Select a source</option>
              {sources.data?.items?.map((item) => (
                <option key={item.id} value={item.id} disabled={!item.valid || !item.enabled}>
                  {item.name || item.id}
                  {!item.valid ? ' · invalid' : !item.enabled ? ' · disabled' : ''}
                </option>
              ))}
            </select>
            {selected && !eligible && (
              <p className="help">The source must be valid, enabled and have no run in progress.</p>
            )}
          </div>
          <div className="field">
            <Label htmlFor="collect-mode">Action</Label>
            <select
              id="collect-mode"
              className="native-select"
              value={mode}
              disabled={mutation.isPending}
              onChange={(event) => {
                const next = event.target.value as RunMode
                setMode(next)
                setMaxPages('')
              }}
            >
              {Object.entries(modeLabels).map(([value, text]) => (
                <option key={value} value={value} disabled={value === 'metadata' && !metadataReady}>
                  {text}
                </option>
              ))}
            </select>
            <p className="help">
              {mode === 'preview'
                ? 'Inspect a sample without adding torrents to the catalog.'
                : mode === 'full'
                  ? 'Read the whole source and replace its published catalog only after completion. Other sources are preserved.'
                  : mode === 'metadata'
                    ? 'Fetch missing fields from this source for existing torrents. This does not download TMDB information or identify films and series.'
                    : 'Import new torrents. The first update automatically starts with a complete import (Full); later updates use Incremental.'}
            </p>
            {mode === 'incremental' && metadataFollowup && (
              <p className="help">
                After successful Incremental or initial Full completion, a separate source-details
                import will fill configured missing fields only for newly inserted native torrents.
                Existing torrents encountered by this run are excluded. No eligible new torrents
                means no follow-up.
              </p>
            )}
            {provider && definition.isPending ? (
              <p className="help" role="status">
                Checking available actions…
              </p>
            ) : !provider ? (
              <p className="help">Select a source to see its available actions.</p>
            ) : !definition.isError && !metadataSupported ? (
              <p className="help">
                Fetching source details is unavailable for this source. It requires native
                HTTP/JSON, ID recovery and configured missing-metadata fields; remote catalogs are
                not supported.
              </p>
            ) : null}
          </div>
          <p className="notice" aria-live="polite">
            Applied limits:{' '}
            {Number(maxPages || defaultPages) === 0
              ? 'unlimited pages'
              : `${(maxPages || defaultPages) ?? '…'} pages`}{' '}
            · {Number(maxDuration || 0) === 0 ? 'unlimited duration' : `${maxDuration} seconds`}.
            Imports pause when a limit is reached and can resume from the saved point.
          </p>
          <Disclosure title="Advanced import settings">
            {mode === 'incremental' && (
              <div className="field">
                <Label htmlFor="collect-known-pages">Known pages (X)</Label>
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
                  aria-describedby={`collect-known-help${invalidKnownPages ? ' collect-known-error' : ''}`}
                  disabled={mutation.isPending}
                  onChange={(event) => setKnownPages(event.target.value)}
                />
                <p id="collect-known-help" className="help">
                  Empty = source schedule default
                  {definition.data
                    ? ` (${defaultKnownPages === 0 ? 'off' : defaultKnownPages})`
                    : ''}
                  . Stop each scope after this many consecutive entirely known pages. 0 disables
                  early stopping. Applies only to this run; the source configuration is unchanged.
                </p>
                {invalidKnownPages && (
                  <p id="collect-known-error" role="alert" className="text-sm text-destructive">
                    Enter a whole number from 0 to 10000, or leave empty to use the source default.
                  </p>
                )}
              </div>
            )}
            <div className="field">
              <Label htmlFor="collect-budget">Page budget per attempt</Label>
              <Input
                id="collect-budget"
                type="number"
                min={0}
                max={10000}
                step={1}
                value={maxPages}
                placeholder={defaultPages === undefined ? 'Select a source…' : String(defaultPages)}
                aria-invalid={invalidPages}
                aria-describedby={`collect-budget-help${invalidPages ? ' collect-budget-error' : ''}`}
                disabled={mutation.isPending}
                onChange={(event) => setMaxPages(event.target.value)}
              />
              <p id="collect-budget-help" className="help">
                Empty = source limit
                {defaultPages === undefined
                  ? ''
                  : ` (${defaultPages === 0 ? 'unlimited' : `${defaultPages} pages`})`}
                . 0 explicitly selects unlimited. Full, Incremental and Metadata pause when their
                budget is reached, never as a false success.
              </p>
              {invalidPages && (
                <p id="collect-budget-error" role="alert" className="text-sm text-destructive">
                  Enter a whole number from 0 to 10000, or leave empty to use the source limit.
                </p>
              )}
            </div>
            <div className="field">
              <Label htmlFor="collect-duration">Duration budget per attempt (seconds)</Label>
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
                aria-describedby={`collect-duration-help${invalidDuration ? ' collect-duration-error' : ''}`}
                onChange={(event) => setMaxDuration(event.target.value)}
              />
              <p id="collect-duration-help" className="help">
                Empty or 0 = unlimited duration. Pauses at a safe response boundary or during a
                cooldown wait, without interrupting atomic Full publication.
              </p>
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
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
              disabled={mutation.isPending}
            >
              Back
            </Button>
            <Button
              type="submit"
              disabled={
                mutation.isPending ||
                !eligible ||
                !modeEligible ||
                !definitionReady ||
                invalidPages ||
                invalidDuration ||
                invalidKnownPages
              }
            >
              {mutation.isPending ? 'Creating…' : 'Start import'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
