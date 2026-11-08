import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import { StorageImpact } from '@/components/storage-indexes'
import { FormStatus } from '@/components/form-status'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

const groups = [
  [
    'diagnostics',
    'Source diagnostics (days)',
    'high',
    'medium',
    'Processed records, responses, source events and completed run details. Checkpoints and preparation needed for unfinished imports remain protected.',
  ],
  [
    'changes',
    'Published changes (days)',
    'high',
    'medium',
    'Additions, edits, deletions and their retained field differences. Imported torrents and matching decisions are unaffected.',
  ],
  [
    'logs',
    'Activity logs (days)',
    'medium',
    'low',
    'Application activity and diagnostic messages.',
  ],
  [
    'operations',
    'Operation history (days)',
    'low',
    'low',
    'Finished metadata, identification and debrid operations, including saved download preparation follow-up. Active operations remain protected.',
  ],
  [
    'requests',
    'Stremio request history (days)',
    'low',
    'low',
    'Retained Stremio request results. This does not remove availability from the shared debrid cache.',
  ],
  [
    'sharing',
    'Catalog sharing changes (days)',
    'high',
    'medium',
    'The change journal used for synchronization. The current snapshot needed by active shares is retained; older readers restart with a full synchronization when their cursor expires.',
  ],
] as const
type Settings = {
  revision: number
  days: Record<string, number>
  request_limit: number
  request_results: boolean
}

export function StorageRetentionPanel() {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<{ revision: number; values: Record<string, string> } | null>(
    null,
  )
  const client = useQueryClient()
  const queryKey = ['storage-retention']
  const query = useQuery({
    queryKey,
    enabled: open,
    queryFn: ({ signal }) => api<Settings>('/settings/storage/retention', { signal }),
  })
  const values = draft?.values ?? {
    ...Object.fromEntries(
      Object.entries(query.data?.days ?? {}).map(([key, days]) => [
        key,
        days < 0 ? '' : String(days),
      ]),
    ),
    request_limit: String(query.data?.request_limit ?? 10000),
    request_results: String(query.data?.request_results ?? true),
  }
  function edit(key: string, value: string) {
    setDraft({
      revision: draft?.revision ?? query.data!.revision,
      values: { ...values, [key]: value },
    })
    save.reset()
  }
  const save = useMutation({
    mutationFn: (value: Settings) =>
      api<Settings>('/settings/storage/retention', { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async (value) => {
      await client.cancelQueries({ queryKey })
      client.setQueryData(queryKey, value)
      setDraft(null)
      void client.invalidateQueries({ queryKey })
    },
    onError: () => void client.invalidateQueries({ queryKey }),
  })
  const dirty =
    draft !== null &&
    (values.request_limit !== String(query.data?.request_limit) ||
      values.request_results !== String(query.data?.request_results) ||
      groups.some(
        ([key]) =>
          values[key] !== (query.data?.days[key] === -1 ? '' : String(query.data?.days[key])),
      ))
  const conflict =
    draft !== null &&
    (draft.revision !== query.data?.revision ||
      (save.error instanceof ApiError && save.error.status === 409))
  const invalid =
    !/^\d+$/.test(values.request_limit ?? '') ||
    Number(values.request_limit) > 1000000 ||
    groups.some(
      ([key]) =>
        values[key] !== '' && (!/^\d+$/.test(values[key] ?? '') || Number(values[key]) > 3650),
    )
  useUnsavedChanges(dirty, 'retention overrides', save.isPending)
  return (
    <Disclosure
      title="Retention overrides"
      open={open}
      onOpenChange={setOpen}
      help={
        <>
          <p>
            Leave a field blank to use the default retention in History retention. Enter 0 to remove
            that history progressively during cleanup.
          </p>
          <p>
            Shortening retention permanently removes older history. Published torrents, metadata,
            manual matching and required recovery/synchronization evidence are preserved. PostgreSQL
            may keep freed space allocated for reuse.
          </p>
          <p>
            Storage and performance levels describe the relative impact of retaining more data.
            Cleanup frequency and batch size remain in History retention.
          </p>
        </>
      }
    >
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending
        ? open && <Loading label="Loading retention overrides" />
        : query.data && (
            <form
              aria-label="Retention overrides"
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                if (draft && dirty && !conflict && !invalid)
                  save.mutate({
                    revision: draft.revision,
                    request_limit: Number(values.request_limit),
                    request_results: values.request_results === 'true',
                    days: Object.fromEntries(
                      groups.map(([key]) => [key, values[key] === '' ? -1 : Number(values[key])]),
                    ),
                  })
              }}
            >
              <div className="grid items-start gap-5 md:grid-cols-2">
                {groups.map(([key, label, storage, performance, help]) => (
                  <div key={key} className="field">
                    <Label htmlFor={`storage-retention-${key}`} help={help}>
                      {label}
                    </Label>
                    <Input
                      id={`storage-retention-${key}`}
                      type="number"
                      min={0}
                      max={3650}
                      step={1}
                      value={values[key] ?? ''}
                      placeholder="Default retention"
                      disabled={save.isPending}
                      onChange={(event) => {
                        setDraft({
                          revision: draft?.revision ?? query.data!.revision,
                          values: { ...values, [key]: event.target.value },
                        })
                        save.reset()
                      }}
                    />
                    <StorageImpact storage={storage} performance={performance} />
                  </div>
                ))}
              </div>
              <div className="field max-w-xl">
                <Label
                  htmlFor="storage-request-limit"
                  help="Maximum retained Stremio requests, in addition to their age limit. Use 0 for no count limit. Reductions are applied progressively; cumulative request statistics are retained."
                >
                  Maximum retained Stremio requests
                </Label>
                <Input
                  id="storage-request-limit"
                  type="number"
                  min={0}
                  max={1000000}
                  step={1}
                  value={values.request_limit}
                  disabled={save.isPending}
                  onChange={(event) => edit('request_limit', event.target.value)}
                />
                <StorageImpact storage="high" performance="medium" />
              </div>
              <div className="flex max-w-xl items-start justify-between gap-4">
                <div className="min-w-0 space-y-1">
                  <Label
                    htmlFor="storage-request-results"
                    help="Keep the returned result summaries in request details. Disabling this retains request counts, outcomes and provider calls, but omits new result lists. It does not change the results sent to Stremio or cached availability."
                  >
                    Stremio result details
                  </Label>
                  <StorageImpact storage="high" performance="low" />
                </div>
                <Switch
                  id="storage-request-results"
                  checked={values.request_results === 'true'}
                  disabled={save.isPending}
                  onCheckedChange={(checked) => edit('request_results', String(checked))}
                />
              </div>
              <ErrorNotice error={save.error} />
              {conflict && (
                <p className="notice notice-warning" role="status">
                  Retention overrides changed elsewhere.{' '}
                  <Button
                    type="button"
                    variant="outline"
                    className="ml-3"
                    onClick={() => {
                      setDraft(null)
                      save.reset()
                    }}
                  >
                    Reload saved value
                  </Button>
                </p>
              )}
              <div className="toolbar">
                <Button type="submit" disabled={!dirty || invalid || conflict || save.isPending}>
                  {save.isPending ? 'Saving…' : 'Save'}
                </Button>
                <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
              </div>
            </form>
          )}
    </Disclosure>
  )
}
