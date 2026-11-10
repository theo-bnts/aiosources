import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { bytes } from '@/lib/format'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

type Impact = 'low' | 'medium' | 'high'
export function StorageImpact({
  storage,
  performance,
  benefit = false,
}: {
  storage: Impact
  performance: Impact
  benefit?: boolean
}) {
  return (
    <p className="help">
      Storage: {storage[0].toUpperCase() + storage.slice(1)} · Performance{' '}
      {benefit ? 'benefit' : 'cost'}: {performance[0].toUpperCase() + performance.slice(1)}
    </p>
  )
}

const indexes = [
  [
    'torrents_fulltext',
    'Whole-word torrent search',
    'high',
    'high',
    'Speeds up quoted phrases and whole-word searches. Disabling it saves index space but can make these searches much slower.',
  ],
  [
    'torrents_fragments',
    'Partial torrent search',
    'high',
    'high',
    'Speeds up title fragments and partial info hashes. Disabling it preserves search results but can require a full catalog scan.',
  ],
  [
    'torrents_categories',
    'Torrent category filters',
    'low',
    'high',
    'Speeds up category filtering in Files and Torznab.',
  ],
  [
    'torrents_size',
    'Torrent size filters and sorting',
    'medium',
    'high',
    'Speeds up size ranges and size ordering.',
  ],
  [
    'torrents_published_at',
    'Torrent date filters and sorting',
    'medium',
    'high',
    'Speeds up publication-date filtering and ordering.',
  ],
  [
    'raw_records_fragments',
    'Processed record search',
    'low',
    'low',
    'Speeds up searches within a run’s retained processed records. Its benefit depends on the diagnostic volume retained.',
  ],
] as const

type Settings = {
  revision: number
  values: Record<string, boolean>
  status: { name: string; enabled: boolean; ready: boolean; bytes: number; error?: string }[]
}

export function StorageIndexesPanel() {
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<Settings | null>(null)
  const queryKey = ['storage-indexes']
  const query = useQuery({
    queryKey,
    enabled: open,
    queryFn: ({ signal }) => api<Settings>('/settings/storage/indexes', { signal }),
    refetchInterval: open ? AUTO_REFRESH_INTERVAL : false,
  })
  const save = useMutation({
    mutationFn: (value: Settings) =>
      api<Settings>('/settings/storage/indexes', {
        method: 'PUT',
        body: JSON.stringify({ revision: value.revision, values: value.values }),
      }),
    onSuccess: async (value) => {
      await client.cancelQueries({ queryKey })
      client.setQueryData(queryKey, value)
      setDraft(null)
      void client.invalidateQueries({ queryKey })
    },
    onError: () => void client.invalidateQueries({ queryKey }),
  })
  const value = draft ?? query.data
  const dirty =
    draft !== null && indexes.some(([key]) => draft.values[key] !== query.data?.values[key])
  const conflict =
    draft !== null &&
    (draft.revision !== query.data?.revision ||
      (save.error instanceof ApiError && save.error.status === 409))
  useUnsavedChanges(dirty, 'search indexes', save.isPending)
  return (
    <Disclosure
      title="Search indexes"
      open={open}
      onOpenChange={setOpen}
      help={
        <>
          <p>
            Optional accelerators only. Required identity, recovery, matching and synchronization
            indexes remain protected.
          </p>
          <p>
            Changes are applied in the background without resetting or reimporting torrents.
            Rebuilding an index can take time and temporarily needs extra disk space. PostgreSQL
            continues serving reads and imports.
          </p>
          <p>
            Storage is the space used by the index; performance is its search-speed benefit. Levels
            are indicative and based on local catalog measurements. Actual allocated space appears
            for each index.
          </p>
        </>
      }
    >
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending
        ? open && <Loading label="Loading search indexes" />
        : value && (
            <form
              aria-label="Search indexes"
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                if (dirty && !conflict) save.mutate(value)
              }}
            >
              <div className="space-y-5">
                {indexes.map(([key, label, storage, performance, help]) => {
                  const status = query.data?.status.find((item) => item.name === key)
                  return (
                    <div key={key} className="flex items-start justify-between gap-4">
                      <div className="min-w-0 space-y-1">
                        <Label htmlFor={`storage-index-${key}`} help={help}>
                          {label}
                        </Label>
                        <StorageImpact storage={storage} performance={performance} benefit />
                        <p className="help">
                          {bytes(status?.bytes ?? 0)}
                          {status && !status.ready && ' · Change pending'}
                        </p>
                        {status?.error && (
                          <p className="help text-warning-foreground" role="status">
                            {status.error}
                          </p>
                        )}
                      </div>
                      <Switch
                        id={`storage-index-${key}`}
                        checked={value.values[key]}
                        disabled={save.isPending}
                        onCheckedChange={(checked) => {
                          setDraft({ ...value, values: { ...value.values, [key]: checked } })
                          save.reset()
                        }}
                      />
                    </div>
                  )
                })}
              </div>
              <ErrorNotice error={save.error} />
              {conflict && (
                <p className="notice notice-warning" role="status">
                  Search indexes changed elsewhere.{' '}
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
                <Button type="submit" disabled={!dirty || conflict || save.isPending}>
                  {save.isPending ? 'Saving…' : 'Save'}
                </Button>
                <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
              </div>
            </form>
          )}
    </Disclosure>
  )
}
