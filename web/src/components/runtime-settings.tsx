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
    ],
  },
} as const

type Section = keyof typeof sections
type Settings = { revision: number; values: Record<string, number> }
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
  const invalid = definition.fields.some(
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
                    dirty && invalid ? 'Enter values within the indicated ranges.' : undefined
                  }
                />
              </div>
            </form>
          )}
    </Disclosure>
  )
}
