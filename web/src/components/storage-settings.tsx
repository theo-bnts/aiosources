import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ConfirmAction, ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'

type StorageSettings = { mode: 'compact' | 'full'; revision: number }
type StorageOverview = { settings: StorageSettings; running: number }

export function StorageSettingsPanel() {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['storage-settings'],
    queryFn: ({ signal }) => api<StorageOverview>('/settings/storage', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const [draft, setDraft] = useState<StorageSettings | null>(null)
  const save = useMutation({
    mutationFn: (settings: StorageSettings) =>
      api<StorageOverview>('/settings/storage', {
        method: 'PUT',
        body: JSON.stringify(settings),
      }),
    onSuccess: async (result) => {
      await client.cancelQueries({ queryKey: ['storage-settings'] })
      client.setQueryData<StorageOverview>(['storage-settings'], (current) =>
        current && current.settings.revision > result.settings.revision ? current : result,
      )
      setDraft(null)
      void client.invalidateQueries({ queryKey: ['storage-settings'] })
    },
    onError: () => void client.invalidateQueries({ queryKey: ['storage-settings'] }),
  })
  const settings = draft ?? query.data?.settings
  const dirty = draft !== null && draft.mode !== query.data?.settings.mode
  const conflict =
    draft !== null &&
    (draft.revision !== query.data?.settings.revision ||
      (save.error instanceof ApiError && save.error.status === 409))
  useUnsavedChanges(dirty, 'storage settings', save.isPending)

  let resetUnavailableReason: string | undefined
  if (query.error) {
    resetUnavailableReason = 'Storage settings could not be checked. Try again above.'
  } else if (!query.data) {
    resetUnavailableReason = 'Wait for storage settings to load.'
  } else if (save.isPending) {
    resetUnavailableReason = 'Wait for storage settings to finish saving.'
  } else if (conflict) {
    resetUnavailableReason = 'Reload the saved storage settings before resetting source data.'
  } else if (dirty) {
    resetUnavailableReason = 'Save the selected storage mode before resetting source data.'
  } else if (query.data.running > 0) {
    resetUnavailableReason = 'Pause running imports before resetting source data.'
  }

  async function reset() {
    if (!query.data) return
    await api<StorageOverview>('/settings/storage/reset-sources', {
      method: 'POST',
      body: JSON.stringify({ revision: query.data.settings.revision, confirm: true }),
    })
    await client.invalidateQueries()
  }

  return (
    <section className="section-card">
      <h2 className="section-title">Data retention</h2>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading storage settings" />
      ) : (
        settings && (
          <>
            <form
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                if (dirty && !conflict) save.mutate(settings)
              }}
            >
              <div className="max-w-xl space-y-2">
                <Label htmlFor="storage-mode">Stored torrent information</Label>
                <select
                  className="native-select w-full"
                  id="storage-mode"
                  value={settings.mode}
                  disabled={save.isPending}
                  onChange={(event) => {
                    setDraft({
                      ...settings,
                      mode: event.target.value as StorageSettings['mode'],
                    })
                    save.reset()
                  }}
                >
                  <option value="compact">Storage saving (recommended)</option>
                  <option value="full">All mapped fields</option>
                </select>
                <p className="help">
                  Storage saving keeps identifiers and release/playback information used by
                  AIOSources and AIOStreams. Unused source details and successful record diagnostics
                  are omitted; errors, run history and TMDB matching data are retained.
                </p>
                <p className="help">
                  Applies to imports started after saving. Existing imports keep their mode, and
                  previously stored data is not reduced automatically.
                </p>
              </div>
              <ErrorNotice error={save.error} />
              {conflict && (
                <p className="notice notice-warning" role="status">
                  Storage settings changed elsewhere. Reload the saved value before saving.
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
            <div className="border-t border-border pt-5">
              <ConfirmAction
                variant="destructive"
                title="Reset all source data?"
                label="Reset source data"
                description="All imported torrents, remote catalog copies, source history, resume points and manual identifications will be removed. Source definitions, secrets, schedules, sharing permissions, TMDB information and debrid caches are retained. The next import starts from scratch using the saved storage mode. This cannot be undone."
                action={reset}
                trigger={
                  <Button
                    variant="destructive"
                    disabled={Boolean(resetUnavailableReason)}
                    aria-describedby="source-data-reset-help source-data-reset-status"
                  >
                    Reset all source data
                  </Button>
                }
              />
              <p className="help mt-2" id="source-data-reset-help">
                Optional: start over to reclaim space used by previously stored source data. Queued
                and paused imports are removed with their data.
              </p>
              <p
                className={resetUnavailableReason ? 'help mt-2 text-amber-300' : 'sr-only'}
                id="source-data-reset-status"
                role="status"
                aria-live="polite"
              >
                {resetUnavailableReason}
              </p>
            </div>
          </>
        )
      )}
    </section>
  )
}
