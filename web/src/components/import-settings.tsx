import { Disclosure } from '@/components/ui/disclosure'
import { importNumberFields as numberFields } from '@/lib/import-settings-definitions'
import { Switch } from '@/components/ui/switch'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import type { CollectionOverview, CollectionSettings } from '@/lib/collection-types'
import { ErrorNotice, Loading } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { FormStatus } from '@/components/form-status'

type NumberField = keyof typeof numberFields
type FormValues = Record<NumberField, string> & {
  auto_resume_interrupted: boolean
  no_progress_action: CollectionSettings['no_progress_action']
}
type Draft = { values: FormValues; base: CollectionSettings }
const groups = {
  concurrency: { title: 'Concurrency', keys: ['workers'], help: 'Applies immediately.' },
  quota: {
    title: 'Quota handling',
    keys: ['max_quota_retries', 'max_quota_wait_seconds'],
    help: 'Defaults captured by new imports. Existing runs keep their saved policy.',
  },
  progress: {
    title: 'Useful progress',
    keys: ['no_progress_requests', 'no_progress_action'],
    help: 'Defaults captured by new imports. Existing runs keep their saved policy.',
  },
  recovery: { title: 'Recovery', keys: ['auto_resume_interrupted'], help: 'Applies immediately.' },
} as const

type Scope = keyof typeof groups
const scopes = Object.keys(groups) as Scope[]
const numberKeys = Object.keys(numberFields) as NumberField[]

function formValues(settings: CollectionSettings): FormValues {
  return {
    auto_resume_interrupted: settings.auto_resume_interrupted ?? true,
    workers: String(settings.workers),
    max_quota_retries: String(settings.max_quota_retries),
    max_quota_wait_seconds: String(settings.max_quota_wait_seconds),
    no_progress_requests: String(settings.no_progress_requests),
    no_progress_action: settings.no_progress_action,
  }
}
function changed(scope: Scope, draft: Draft | undefined) {
  if (!draft) return false
  const saved = formValues(draft.base)
  return groups[scope].keys.some((key) => draft.values[key] !== saved[key])
}
// Publish only the selected block. Other local drafts and concurrent settings
// must never be copied into this write just because the endpoint accepts all fields.
function blockSettings(scope: Scope, draft: Draft): CollectionSettings {
  const result = { ...draft.base }
  for (const key of groups[scope].keys) {
    if (key === 'auto_resume_interrupted') result[key] = draft.values[key]
    else if (key === 'no_progress_action') result[key] = draft.values[key]
    else result[key] = Number(draft.values[key])
  }
  return result
}

export function ImportSettingsPanel({ active }: { active: boolean }) {
  const client = useQueryClient()
  const queryKey = ['collection-settings']
  const overview = useQuery({
    queryKey,
    enabled: active,
    queryFn: ({ signal }) => api<CollectionOverview>('/settings/collections', { signal }),
  })
  const [drafts, setDrafts] = useState<Partial<Record<Scope, Draft>>>({})
  const [feedback, setFeedback] = useState<
    Partial<Record<Scope, { error?: unknown; saved?: boolean }>>
  >({})
  const save = useMutation({
    mutationFn: ({ value }: { scope: Scope; value: CollectionSettings }) =>
      api<CollectionOverview>('/settings/collections', {
        method: 'PUT',
        body: JSON.stringify(value),
      }),
    onMutate: () => client.cancelQueries({ queryKey }),
    onSuccess: async (result, submitted) => {
      await client.cancelQueries({ queryKey })
      const current = client.getQueryData<CollectionOverview>(queryKey)
      const latest =
        current && current.settings.revision > result.settings.revision ? current : result
      client.setQueryData(queryKey, latest)
      setDrafts((previous) => {
        const next = { ...previous }
        delete next[submitted.scope]
        if (latest.settings.revision === submitted.value.revision + 1) {
          for (const scope of scopes)
            if (next[scope]) next[scope] = { ...next[scope]!, base: latest.settings }
        }
        return next
      })
      setFeedback((current) => ({ ...current, [submitted.scope]: { saved: true } }))
      void client.invalidateQueries({ queryKey })
    },
    onError: (error, submitted) => {
      setFeedback((current) => ({ ...current, [submitted.scope]: { error } }))
      if (error instanceof ApiError && error.status === 409)
        void client.invalidateQueries({ queryKey })
    },
  })
  const data = overview.data
  useUnsavedChanges(
    scopes.some((scope) => changed(scope, drafts[scope])),
    'import execution settings',
    save.isPending,
  )

  function change(scope: Scope, patch: Partial<FormValues>) {
    if (!data) return
    setDrafts((current) => {
      const previous = current[scope]
      const draft = {
        values: { ...(previous?.values ?? formValues(data.settings)), ...patch },
        base: previous?.base ?? data.settings,
      }
      const next = { ...current, [scope]: draft }
      if (!changed(scope, draft)) delete next[scope]
      return next
    })
    setFeedback((current) => ({ ...current, [scope]: undefined }))
  }

  return (
    <section className="space-y-5" aria-label="Import execution">
      <ErrorNotice error={overview.error} retry={() => void overview.refetch()} />
      {overview.isPending ? (
        <Loading label="Loading import settings" />
      ) : (
        data &&
        scopes.map((scope) => {
          const group = groups[scope]
          const draft = drafts[scope]
          const values = draft?.values ?? formValues(data.settings)
          const dirty = changed(scope, draft)
          const error = feedback[scope]?.error
          const rejected = error instanceof ApiError && error.status === 409
          const conflict = Boolean(
            draft && (rejected || data.settings.revision !== draft.base.revision),
          )
          const latestAvailable = Boolean(draft && data.settings.revision > draft.base.revision)
          const errors: Partial<Record<NumberField, string>> = {}
          for (const key of numberKeys) {
            if (!(group.keys as readonly string[]).includes(key)) continue
            const field = numberFields[key]
            const value = Number(values[key])
            if (
              !/^\d+$/.test(values[key]) ||
              !Number.isSafeInteger(value) ||
              value < field.min ||
              value > field.max
            )
              errors[key] = `Enter a whole number from ${field.min} to ${field.max}.`
          }
          const invalid = Object.keys(errors).length > 0
          function numberInput(key: NumberField) {
            const field = numberFields[key]
            const id = `collection-${key}`
            return (
              <div className="min-w-0 space-y-2" key={key}>
                <Label
                  htmlFor={id}
                  help={`${field.help} Range: ${field.min}–${field.max}. Currently saved: ${data!.settings[key]}.`}
                >
                  {field.label}
                </Label>
                <Input
                  id={id}
                  name={key}
                  type="number"
                  min={field.min}
                  max={field.max}
                  step={1}
                  required
                  value={values[key]}
                  disabled={save.isPending}
                  aria-invalid={Boolean(errors[key])}
                  aria-describedby={errors[key] ? `${id}-error` : undefined}
                  onChange={(event) => change(scope, { [key]: event.target.value })}
                />
                {errors[key] && (
                  <p id={`${id}-error`} role="alert" className="text-sm text-destructive">
                    {errors[key]}
                  </p>
                )}
              </div>
            )
          }
          return (
            <Disclosure key={scope} title={group.title} help={group.help}>
              <form
                aria-label={group.title}
                className="space-y-5"
                noValidate
                onSubmit={(event) => {
                  event.preventDefault()
                  if (draft && dirty && !save.isPending && !conflict && !invalid)
                    save.mutate({ scope, value: blockSettings(scope, draft) })
                }}
              >
                <div
                  className={
                    scope === 'concurrency' ? 'max-w-sm' : 'grid items-start gap-5 md:grid-cols-2'
                  }
                >
                  {numberKeys
                    .filter((key) => (group.keys as readonly string[]).includes(key))
                    .map(numberInput)}
                  {scope === 'progress' && (
                    <div className="space-y-2">
                      <Label
                        htmlFor="collection-no-progress-action"
                        help="Send one warning per streak. New source IDs reset the streak. A pause requires manual resume, which grants a fresh allowance without forgetting seen IDs. A completed traversal is not paused. This action has no effect when the threshold is 0."
                      >
                        At the no-progress threshold
                      </Label>
                      <select
                        id="collection-no-progress-action"
                        name="no_progress_action"
                        className="native-select w-full"
                        value={values.no_progress_action}
                        disabled={save.isPending}
                        onChange={(event) =>
                          change(scope, {
                            no_progress_action: event.target
                              .value as CollectionSettings['no_progress_action'],
                          })
                        }
                      >
                        <option value="warn">Warn and continue</option>
                        <option value="pause">Warn and pause</option>
                      </select>
                    </div>
                  )}
                  {scope === 'recovery' && (
                    <div className="flex items-center gap-3 md:col-span-2">
                      <Switch
                        id="collection-auto-resume"
                        checked={values.auto_resume_interrupted}
                        disabled={save.isPending}
                        onCheckedChange={(value) =>
                          change(scope, { auto_resume_interrupted: value })
                        }
                      />
                      <Label
                        htmlFor="collection-auto-resume"
                        help="Resume from the last committed checkpoint after a process or connection failure. Manual pauses, quota holds, cancellations and configuration failures still require attention. When disabled, interrupted imports remain paused for manual resume."
                      >
                        Automatically resume interrupted imports
                      </Label>
                    </div>
                  )}
                </div>
                {conflict && (
                  <div className="notice notice-warning space-y-3" role="alert">
                    <p>
                      These settings changed while you were editing. Your unsaved draft is
                      preserved.
                      {latestAvailable
                        ? ' Choose whether to use the saved values or keep your edits to this block. Other blocks are preserved.'
                        : ' Refresh the saved settings before choosing how to continue.'}
                    </p>
                    <div className="toolbar">
                      {latestAvailable ? (
                        <>
                          <Button
                            type="button"
                            variant="warning"
                            disabled={save.isPending}
                            onClick={() => {
                              setDrafts((current) => {
                                const next = { ...current }
                                delete next[scope]
                                return next
                              })
                              setFeedback((current) => ({ ...current, [scope]: undefined }))
                            }}
                          >
                            Use saved values
                          </Button>
                          <Button
                            type="button"
                            variant="outline"
                            disabled={save.isPending}
                            onClick={() => {
                              setDrafts((current) => ({
                                ...current,
                                [scope]: { values, base: data.settings },
                              }))
                              setFeedback((current) => ({ ...current, [scope]: undefined }))
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
                {!rejected && <ErrorNotice error={error} />}
                <div className="flex flex-wrap items-center gap-4">
                  <Button type="submit" disabled={!dirty || invalid || save.isPending || conflict}>
                    {save.isPending && save.variables.scope === scope ? 'Saving…' : 'Save'}
                  </Button>
                  <FormStatus
                    dirty={dirty}
                    saving={save.isPending && save.variables.scope === scope}
                    saved={feedback[scope]?.saved}
                    message={invalid ? 'Correct the highlighted values before saving.' : undefined}
                  />
                </div>
              </form>
            </Disclosure>
          )
        })
      )}
    </section>
  )
}
