import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import type { CollectionOverview, CollectionSettings } from '@/lib/collection-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { FormStatus } from '@/components/form-status'

const numberFields = {
  workers: {
    label: 'Simultaneous imports',
    min: 1,
    max: 32,
    help: 'Shared maximum across this database. Per-source request pacing is unchanged.',
  },
  max_quota_retries: {
    label: 'Automatic retries after HTTP 429',
    min: 0,
    max: 10,
    help: '0 = no automatic retry. Server quota deadlines are always respected.',
  },
  max_quota_wait_seconds: {
    label: 'Maximum automatic quota wait (seconds)',
    min: 0,
    max: 86400,
    help: '0 = unlimited. A longer server-requested wait pauses the run after saving its status and deadline, not its response body. Explicit Resume accepts that wait without shortening the server deadline.',
  },
  no_progress_requests: {
    label: 'Responses without new source IDs',
    min: 0,
    max: 100000,
    help: '0 = disabled. Counts successful data responses without a previously unseen native ID, not quota errors or auxiliary/control responses.',
  },
} as const

type NumberField = keyof typeof numberFields
type FormValues = Record<NumberField, string> & {
  no_progress_action: CollectionSettings['no_progress_action']
}
type Draft = { values: FormValues; base: CollectionSettings }
const numberKeys = Object.keys(numberFields) as NumberField[]

function formValues(settings: CollectionSettings): FormValues {
  return {
    workers: String(settings.workers),
    max_quota_retries: String(settings.max_quota_retries),
    max_quota_wait_seconds: String(settings.max_quota_wait_seconds),
    no_progress_requests: String(settings.no_progress_requests),
    no_progress_action: settings.no_progress_action,
  }
}

export function ImportSettingsPanel({ active }: { active: boolean }) {
  const client = useQueryClient()
  const overview = useQuery({
    queryKey: ['collection-settings'],
    enabled: active,
    queryFn: ({ signal }) => api<CollectionOverview>('/settings/collections', { signal }),
  })
  const [draft, setDraft] = useState<Draft | null>(null)
  const save = useMutation({
    mutationFn: (settings: CollectionSettings) =>
      api<CollectionOverview>('/settings/collections', {
        method: 'PUT',
        body: JSON.stringify(settings),
      }),
    onMutate: () => client.cancelQueries({ queryKey: ['collection-settings'] }),
    onSuccess: async (result) => {
      await client.cancelQueries({ queryKey: ['collection-settings'] })
      client.setQueryData<CollectionOverview>(['collection-settings'], (current) =>
        current && current.settings.revision > result.settings.revision ? current : result,
      )
      setDraft(null)
      void client.invalidateQueries({ queryKey: ['collection-settings'] })
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        void client.invalidateQueries({ queryKey: ['collection-settings'] })
      }
    },
  })
  const data = overview.data
  const values = draft?.values ?? (data ? formValues(data.settings) : undefined)
  const dirty =
    draft !== null &&
    (numberKeys.some((key) => draft.values[key] !== String(draft.base[key])) ||
      draft.values.no_progress_action !== draft.base.no_progress_action)
  useUnsavedChanges(dirty, 'advanced settings', save.isPending)
  const rejected = save.error instanceof ApiError && save.error.status === 409
  const conflict =
    draft !== null && (rejected || (data && data.settings.revision !== draft.base.revision))
  const latestAvailable =
    draft !== null && data !== undefined && data.settings.revision > draft.base.revision
  const errors: Partial<Record<NumberField, string>> = {}
  if (values) {
    for (const key of numberKeys) {
      const field = numberFields[key]
      const value = Number(values[key])
      if (
        !/^\d+$/.test(values[key]) ||
        !Number.isSafeInteger(value) ||
        value < field.min ||
        value > field.max
      ) {
        errors[key] = `Enter a whole number from ${field.min} to ${field.max}.`
      }
    }
  }
  const invalid = Object.keys(errors).length > 0

  function change(patch: Partial<FormValues>) {
    if (!data || !values) return
    setDraft({ values: { ...values, ...patch }, base: draft?.base ?? data.settings })
    if (!rejected) save.reset()
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!draft || !dirty || save.isPending || conflict || invalid) return
    const value = draft.values
    save.mutate({
      workers: Number(value.workers),
      max_quota_retries: Number(value.max_quota_retries),
      max_quota_wait_seconds: Number(value.max_quota_wait_seconds),
      no_progress_requests: Number(value.no_progress_requests),
      no_progress_action: value.no_progress_action,
      revision: draft.base.revision,
    })
  }

  function useSavedValues() {
    setDraft(null)
    save.reset()
  }

  function numberInput(key: NumberField) {
    const field = numberFields[key]
    const id = `collection-${key}`
    return (
      <div className="min-w-0 space-y-2" key={key}>
        <Label htmlFor={id}>{field.label}</Label>
        <Input
          id={id}
          name={key}
          type="number"
          min={field.min}
          max={field.max}
          step={1}
          required
          value={values?.[key] ?? ''}
          disabled={save.isPending}
          aria-invalid={Boolean(errors[key])}
          aria-describedby={`${id}-help${errors[key] ? ` ${id}-error` : ''}`}
          onChange={(event) => change({ [key]: event.target.value })}
        />
        <p id={`${id}-help`} className="text-xs leading-5 text-muted-foreground">
          {field.help} Range: {field.min}–{field.max}. Currently saved: {data?.settings[key]}.
        </p>
        {errors[key] && (
          <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
            {errors[key]}
          </p>
        )}
      </div>
    )
  }

  return (
    <section className="space-y-5" aria-labelledby="collection-settings-heading">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-2">
          <h2 id="collection-settings-heading" className="font-medium">
            Import execution
          </h2>
        </div>
      </div>
      <ErrorNotice error={overview.error} retry={() => void overview.refetch()} />
      {overview.isPending ? (
        <Loading label="Loading advanced settings" />
      ) : (
        data &&
        values && (
          <>
            <form onSubmit={submit} aria-label="Advanced settings" className="space-y-6" noValidate>
              <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
                <legend className="pr-3 text-sm font-medium">Concurrency · Immediate</legend>
                <div className="max-w-sm">{numberInput('workers')}</div>
              </fieldset>
              <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
                <legend className="pr-3 text-sm font-medium">Quota handling · New imports</legend>
                <div className="grid items-start gap-5 md:grid-cols-2">
                  {numberInput('max_quota_retries')}
                  {numberInput('max_quota_wait_seconds')}
                </div>
              </fieldset>
              <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
                <legend className="pr-3 text-sm font-medium">Useful progress · New imports</legend>
                <div className="grid items-start gap-5 md:grid-cols-2">
                  {numberInput('no_progress_requests')}
                  <div className="space-y-2">
                    <Label htmlFor="collection-no-progress-action">
                      At the no-progress threshold
                    </Label>
                    <select
                      id="collection-no-progress-action"
                      name="no_progress_action"
                      className="native-select w-full"
                      value={values.no_progress_action}
                      disabled={save.isPending}
                      aria-describedby="collection-no-progress-action-help"
                      onChange={(event) =>
                        change({
                          no_progress_action: event.target
                            .value as CollectionSettings['no_progress_action'],
                        })
                      }
                    >
                      <option value="warn">Warn and continue</option>
                      <option value="pause">Warn and pause</option>
                    </select>
                    <p
                      id="collection-no-progress-action-help"
                      className="text-xs leading-5 text-muted-foreground"
                    >
                      Send one warning per streak. New source IDs reset the streak. A pause requires
                      manual resume, which grants a fresh allowance without forgetting seen IDs. A
                      completed traversal is not paused. This action has no effect when the
                      threshold is 0.
                    </p>
                  </div>
                </div>
              </fieldset>
              {conflict && (
                <div className="notice notice-warning space-y-3" role="alert">
                  <p>
                    These settings changed while you were editing. Your unsaved draft is preserved.
                    {latestAvailable
                      ? ' Choose whether to use the saved settings or keep your entire draft before saving again. Keeping your draft will replace all saved advanced settings on the next save.'
                      : ' Refresh the saved settings before choosing how to continue.'}
                  </p>
                  <div className="toolbar">
                    {latestAvailable ? (
                      <>
                        <Button
                          type="button"
                          variant="warning"
                          disabled={save.isPending}
                          onClick={useSavedValues}
                        >
                          Use saved values
                        </Button>
                        <Button
                          type="button"
                          variant="outline"
                          disabled={save.isPending}
                          onClick={() => {
                            setDraft({ values, base: data.settings })
                            save.reset()
                          }}
                        >
                          Keep my draft
                        </Button>
                      </>
                    ) : (
                      <Button
                        type="button"
                        variant="outline"
                        disabled={overview.isFetching}
                        onClick={() => void overview.refetch()}
                      >
                        {overview.isFetching ? 'Loading…' : 'Load latest settings'}
                      </Button>
                    )}
                  </div>
                </div>
              )}
              {!rejected && <ErrorNotice error={save.error} />}
              <div className="flex flex-wrap items-center gap-4 border-t border-border pt-5">
                <Button
                  type="submit"
                  disabled={!dirty || invalid || save.isPending || Boolean(conflict)}
                >
                  {save.isPending ? 'Saving…' : 'Save'}
                </Button>
                {draft && !conflict && (
                  <Button
                    type="button"
                    variant="warning"
                    onClick={useSavedValues}
                    disabled={save.isPending}
                  >
                    Discard changes
                  </Button>
                )}
                <FormStatus
                  dirty={dirty}
                  saving={save.isPending}
                  saved={save.isSuccess}
                  message={invalid ? 'Correct the highlighted values before saving.' : undefined}
                  savedMessage="Advanced settings saved. Each setting applies at the scope shown above."
                />
              </div>
            </form>
          </>
        )
      )}
    </section>
  )
}
