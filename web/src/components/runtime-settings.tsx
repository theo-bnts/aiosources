import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'

const sections = {
  debrid: {
    title: 'Operation capacity',
    help: 'Limits per application process. Active work finishes when a limit is reduced; new operations wait or remain available for retry. Each cache update checks a requested title and may make several provider calls. Provider request pacing is unchanged.',
    fields: [
      ['cache_jobs', 'Simultaneous cache updates', 1, 32],
      ['download_jobs', 'Simultaneous download preparations', 1, 32],
      ['cleanup_jobs', 'Simultaneous cleanup operations', 1, 32],
      ['public_checks', 'Simultaneous public TorBox checks', 1, 16],
    ],
  },
  outgoing: {
    title: 'Request timeouts',
    help: 'Maximum time for each outgoing request, including its response body. Source timeouts remain configurable in each source. Changes apply to new requests. The remaining time budget of an operation can be shorter.',
    fields: [
      ['metadata_seconds', 'Metadata request timeout (seconds)', 5, 600],
      ['debrid_seconds', 'Debrid request timeout (seconds)', 5, 600],
      ['inventory_seconds', 'Metadata inventory download timeout (seconds)', 30, 3600],
    ],
  },
  connections: {
    title: 'Request timeouts',
    help: 'Maximum processing and delivery time for new incoming requests, shared by private and public connections. A provider request also respects its outgoing timeout. This does not change the lifetime of an active file download.',
    fields: [
      ['stremio_seconds', 'Stremio request timeout (seconds)', 10, 600],
      ['torznab_seconds', 'Torznab request timeout (seconds)', 5, 600],
      ['catalog_seconds', 'Shared catalog request timeout (seconds)', 5, 600],
    ],
  },
  history: {
    title: 'History retention',
    help: 'Expired diagnostics are removed progressively. Published data and evidence needed to resume imports are preserved. Shortening retention can permanently remove older history; increasing it cannot restore deleted history. Stremio history also keeps its 10,000-request safety limit.',
    fields: [
      ['days', 'Keep history (days)', 1, 3650],
      ['cleanup_seconds', 'Cleanup interval (seconds)', 5, 3600],
      ['batch_size', 'Rows per table cleanup batch', 100, 20000],
      ['pass_seconds', 'Maximum cleanup pass (seconds)', 1, 60],
    ],
  },
  identification: {
    title: 'Identification processing',
    help: 'Controls local identification, independently of the TMDB request rate. Use 0 workers for automatic capacity based on available database connections. Smaller batches reduce memory and checkpoint more often. Changes apply to the next batch; active batches finish.',
    fields: [
      ['workers', 'Maximum identification workers (0 = automatic)', 0, 32],
      ['queue_batch', 'Queued torrents per pass', 10, 2000],
      ['transaction_batch', 'Torrents per saved batch', 1, 100],
    ],
  },
  matching: {
    title: 'Matching reliability',
    help: 'Higher values make automatic identification more conservative. Saving rechecks automatic results; manual reviews are preserved. Conflicting identifiers, incompatible years and invalid numbering remain rejected. Pending rechecks run automatically when identification is enabled.',
    fields: [
      ['minimum_similarity', 'Minimum close-title similarity (%)', 92, 100],
      ['ambiguity_margin', 'Ambiguity margin (%)', 8, 25],
    ],
  },
  metadata: {
    title: 'Information processing',
    help: 'Controls concurrent information requests and bounded processing batches. The configured TMDB request rate still applies. Smaller saved batches reduce memory and commit progress more often. Changes apply to the next batch.',
    fields: [
      ['workers', 'Simultaneous information requests', 1, 32],
      ['queue_batch', 'Information records per pass', 1, 1000],
      ['publication_batch', 'Information records per saved batch', 1, 100],
    ],
  },
  metadata_retries: {
    title: 'Information retries',
    help: 'Temporary provider errors use a limited number of immediate attempts, then a saved retry clock. Delays increase up to the configured maximum; provider quota deadlines are always respected. Authentication errors are not retried within the same request.',
    fields: [
      ['request_attempts', 'Attempts per information request', 1, 10],
      ['request_retry_seconds', 'Initial request retry delay (seconds)', 1, 60],
      ['sync_retry_seconds', 'Initial update retry delay (seconds)', 10, 3600],
      ['sync_retry_max_seconds', 'Maximum update retry delay (seconds)', 60, 86400],
      ['detail_retry_seconds', 'Initial deferred-record retry delay (seconds)', 10, 3600],
      ['detail_retry_max_seconds', 'Maximum deferred-record retry delay (seconds)', 60, 604800],
    ],
  },
  debrid_retries: {
    title: 'Availability retries',
    help: 'Controls retries for incomplete availability checks. Delays increase up to the maximum; valid cached results remain usable. Temporary-item cleanup keeps its ownership checks and manual attention for failed deletions.',
    fields: [
      ['delay_minutes', 'Initial availability retry delay (minutes)', 1, 1440],
      ['maximum_hours', 'Maximum availability retry delay (hours)', 1, 168],
    ],
  },
  source_retries: {
    title: 'Temporary source errors',
    help: 'New imports capture these settings for temporary GET/HEAD failures, including HTTP 500, 502, 503, 504 and transient HTML responses. Existing imports keep their original retry policy across pauses and restarts. Source pacing, quota deadlines and separately configured HTTP 429 handling remain enforced. A retry count of 0 keeps the failed response for manual recovery.',
    fields: [
      ['retries', 'Automatic retries after a temporary source error', 0, 10],
      ['initial_seconds', 'Initial source retry delay (seconds)', 1, 60],
      ['maximum_seconds', 'Maximum source retry delay (seconds)', 1, 900],
    ],
  },
  downloads: {
    title: 'Browser downloads',
    help: 'Applies to public and administrator downloads through this application. Transfer duration starts when a download begins. Link validity controls how long a newly generated link can be opened; existing links retain their expiry. Session revocation, public-access changes and destination checks still apply.',
    fields: [
      ['duration_hours', 'Maximum transfer duration (hours)', 1, 168],
      ['ticket_minutes', 'New download link validity (minutes)', 1, 60],
    ],
  },
  security: {
    title: 'Authentication settings',
    help: 'Session duration applies to new sign-ins. Reducing the session limit does not revoke existing sessions. Sign-in limits apply per minute across application replicas; global capacity must cover the per-address limit. Two-step enrollment duration applies to new setups. Password, CSRF and factor replay protections remain enforced.',
    fields: [
      ['session_hours', 'New administrator session duration (hours)', 1, 168],
      ['session_limit', 'Maximum active administrator sessions', 1, 1024],
      ['login_per_minute', 'Sign-in attempts per address per minute', 1, 60],
      ['login_global_per_minute', 'Total sign-in attempts per minute', 1, 600],
      ['enrollment_minutes', 'Two-step setup validity (minutes)', 5, 30],
    ],
  },
  payloads: {
    title: 'Configuration limits',
    help: 'Limits newly saved source definitions and newly generated public links. Existing definitions and links remain readable. Smaller limits reduce accepted configuration sizes; protocol ceilings, encryption, TLS validation and destination restrictions remain enforced.',
    fields: [
      ['source_kib', 'Source definition limit (KiB)', 16, 128],
      ['public_stremio_bytes', 'Public Search / Stremio configuration limit (bytes)', 1024, 8192],
      ['public_torznab_bytes', 'Public Torznab configuration limit (bytes)', 1024, 6000],
    ],
  },
} as const

type Section = keyof typeof sections
type Settings = { revision: number; values: Record<string, number>; automatic_workers?: number }
type Draft = { base: Settings; values: Record<string, string> }

export function RuntimeSettingsPanel({ section }: { section: Section }) {
  const definition = sections[section]
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<Draft | null>(null)
  const client = useQueryClient()
  const queryKey = ['runtime-settings', section]
  const path = `/settings/runtime/${section}`
  const query = useQuery({
    queryKey,
    enabled: open,
    queryFn: ({ signal }) => api<Settings>(path, { signal }),
  })
  const save = useMutation({
    mutationFn: (value: Settings) =>
      api<Settings>(path, { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async (result) => {
      await client.cancelQueries({ queryKey })
      client.setQueryData<Settings>(queryKey, (current) =>
        current && current.revision > result.revision ? current : result,
      )
      setDraft(null)
      void client.invalidateQueries({ queryKey })
    },
    onError: () => void client.invalidateQueries({ queryKey }),
  })
  const values =
    draft?.values ??
    Object.fromEntries(
      Object.entries(query.data?.values ?? {}).map(([key, value]) => [key, String(value)]),
    )
  const dirty =
    draft !== null &&
    Object.entries(draft.values).some(([key, value]) => value !== String(draft.base.values[key]))
  const conflict =
    draft !== null &&
    (draft.base.revision !== query.data?.revision ||
      (save.error instanceof ApiError && save.error.status === 409))
  const relationshipIssue =
    section === 'source_retries' && Number(values.initial_seconds) > Number(values.maximum_seconds)
      ? 'The maximum retry delay must cover the initial delay.'
      : section === 'identification' &&
          Number(values.transaction_batch) > Number(values.queue_batch)
        ? 'The saved batch cannot exceed the queued batch.'
        : section === 'metadata' && Number(values.publication_batch) > Number(values.queue_batch)
          ? 'The saved batch cannot exceed the fetched batch.'
          : section === 'metadata_retries' &&
              (Number(values.sync_retry_seconds) > Number(values.sync_retry_max_seconds) ||
                Number(values.detail_retry_seconds) > Number(values.detail_retry_max_seconds))
            ? 'Each maximum retry delay must cover its initial delay.'
            : section === 'debrid_retries' &&
                Number(values.delay_minutes) > Number(values.maximum_hours) * 60
              ? 'The maximum retry delay must cover the initial delay.'
              : section === 'security' &&
                  Number(values.login_per_minute) > Number(values.login_global_per_minute)
                ? 'Total sign-in capacity must cover the per-address limit.'
                : undefined
  const invalid =
    Boolean(relationshipIssue) ||
    definition.fields.some(
      ([key, , min, max]) =>
        !/^\d+$/.test(values[key] ?? '') ||
        !Number.isSafeInteger(Number(values[key])) ||
        Number(values[key]) < min ||
        Number(values[key]) > max,
    )
  useUnsavedChanges(dirty, definition.title.toLowerCase(), save.isPending)
  return (
    <Disclosure title={definition.title} open={open} onOpenChange={setOpen}>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending
        ? open && <Loading label={`Loading ${definition.title.toLowerCase()}`} />
        : query.data && (
            <form
              className="space-y-5"
              aria-label={definition.title}
              onSubmit={(event) => {
                event.preventDefault()
                if (draft && dirty && !invalid && !conflict)
                  save.mutate({
                    revision: draft.base.revision,
                    values: Object.fromEntries(
                      Object.entries(draft.values).map(([key, value]) => [key, Number(value)]),
                    ),
                  })
              }}
            >
              <p className="help">{definition.help}</p>
              {section === 'identification' && query.data.automatic_workers !== undefined && (
                <p className="help">
                  Automatic currently uses {query.data.automatic_workers} workers on this process.
                </p>
              )}
              <div className="grid items-start gap-5 md:grid-cols-2">
                {definition.fields.map(([key, label, min, max]) => {
                  const id = `runtime-${section}-${key}`
                  const fieldInvalid =
                    !/^\d+$/.test(values[key] ?? '') ||
                    !Number.isSafeInteger(Number(values[key])) ||
                    Number(values[key]) < min ||
                    Number(values[key]) > max
                  return (
                    <div className="min-w-0 space-y-2" key={key}>
                      <Label htmlFor={id}>{label}</Label>
                      <Input
                        id={id}
                        name={key}
                        type="number"
                        min={min}
                        max={max}
                        step={1}
                        required
                        value={values[key] ?? ''}
                        disabled={save.isPending}
                        aria-invalid={fieldInvalid}
                        aria-describedby={`${id}-help`}
                        onChange={(event) => {
                          setDraft({
                            base: draft?.base ?? query.data!,
                            values: { ...values, [key]: event.target.value },
                          })
                          save.reset()
                        }}
                      />
                      <p
                        className={fieldInvalid ? 'help text-destructive' : 'help'}
                        id={`${id}-help`}
                      >
                        Range: {min}–{max}.
                      </p>
                    </div>
                  )
                })}
              </div>
              {conflict && (
                <div className="notice notice-warning space-y-3" role="status">
                  <p>
                    These settings changed elsewhere. Load the saved values before editing again.
                  </p>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setDraft(null)
                      save.reset()
                    }}
                  >
                    Use saved values
                  </Button>
                </div>
              )}
              {!conflict && <ErrorNotice error={save.error} />}
              <div className="toolbar border-t border-border pt-5">
                <Button type="submit" disabled={!dirty || invalid || conflict || save.isPending}>
                  {save.isPending ? 'Saving…' : 'Save'}
                </Button>
                <FormStatus
                  dirty={dirty}
                  saving={save.isPending}
                  saved={save.isSuccess}
                  message={
                    dirty && invalid
                      ? (relationshipIssue ?? 'Enter values within the indicated ranges.')
                      : undefined
                  }
                />
              </div>
            </form>
          )}
    </Disclosure>
  )
}
