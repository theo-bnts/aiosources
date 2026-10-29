import { useState } from 'react'
import { runtimeSections, type RuntimeSection } from '@/lib/runtime-settings-definitions'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'

type Settings = { revision: number; values: Record<string, number>; automatic_workers?: number }
type Draft = { base: Settings; values: Record<string, string> }

export function RuntimeSettingsPanel({
  section,
  scopeHelp,
}: {
  section: RuntimeSection
  scopeHelp?: string
}) {
  const definition = runtimeSections[section]
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
              <p className="help">
                {definition.help}
                {scopeHelp && ` ${scopeHelp}`}
              </p>
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
