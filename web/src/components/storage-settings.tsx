import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { SettingsSections } from '@/components/settings-sections'
import { useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { ConfirmAction, ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { MultiSelect } from '@/components/ui/multi-select'
import { StorageIndexesPanel, StorageImpact } from '@/components/storage-indexes'
import { StorageRetentionPanel } from '@/components/storage-retention'
import { Disclosure } from '@/components/ui/disclosure'

type StorageOptions = {
  all_fields: boolean
  additional_fields: string[]
  successful_records: boolean
  record_fields: boolean
  publication_history: boolean
}
type StorageSettings = { mode: 'compact' | 'full'; revision: number; options: StorageOptions }
type StorageOverview = {
  settings: StorageSettings
  running: number
  indexing: boolean
  available_fields: string[]
}

export function StorageSettingsPanel() {
  const location = useLocation()
  const [maintenanceOpen, setMaintenanceOpen] = useState(location.hash === '#maintenance')
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
  const dirty =
    draft !== null && JSON.stringify(draft.options) !== JSON.stringify(query.data?.settings.options)
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
    resetUnavailableReason = 'Save the selected storage settings before resetting source data.'
  } else if (query.data.running > 0) {
    resetUnavailableReason = 'Pause running imports before resetting source data.'
  } else if (query.data.indexing) {
    resetUnavailableReason = 'Wait for search index changes to finish before resetting source data.'
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
    <>
      <section className="section-card">
        <h2 className="section-title">Stored data</h2>
        <p className="help">
          Identifiers, search fields, matching evidence and playback information are always
          retained.
        </p>
        <ErrorNotice error={query.error} retry={() => void query.refetch()} />
        {query.isPending && <Loading label="Loading storage settings" />}
      </section>
      <SettingsSections>
        <Disclosure
          title="Torrent data"
          help={
            <>
              <p>
                Fields and processed-record choices apply to imports started after saving. Existing
                imports retain their saved choices. Published change history switches immediately.
                Required information cannot be disabled.
              </p>
              <p>
                Changes do not rewrite existing torrent data. To populate previously omitted fields
                or remove previously retained fields, reset source data in Maintenance, then import
                again. Diagnostic history expires with its retention policy.
              </p>
              <p>
                Impact levels describe the additional storage and processing cost of keeping each
                optional item. The amount depends on your source fields and import volume.
              </p>
            </>
          }
        >
          {settings && (
            <form
              aria-label="Torrent data"
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                if (dirty && !conflict) save.mutate(settings)
              }}
            >
              <div className="space-y-4">
                {(
                  [
                    [
                      'all_fields',
                      'All mapped source fields',
                      'high',
                      'medium',
                      'Keep additional descriptions and source-specific fields that AIOSources does not use. Identifiers, release details and playback fields are retained with this disabled.',
                    ],
                    [
                      'successful_records',
                      'Successful processed records',
                      'high',
                      'high',
                      'Keep an individual diagnostic row for every successfully processed record. Errors, import checkpoints and coverage checks remain available with this disabled.',
                    ],
                    [
                      'record_fields',
                      'Processed record field copies',
                      'high',
                      'medium',
                      'Keep interpreted fields alongside retained diagnostics. This can duplicate torrent information. Original response bodies are never archived.',
                    ],
                    [
                      'publication_history',
                      'Published change history',
                      'high',
                      'medium',
                      'Record additions, edits and deletions with their field differences. Disabling this stops new published-change history immediately; import events, matching and catalog sharing continue. Existing history expires normally.',
                    ],
                  ] as const
                ).map(([key, label, storage, performance, help]) => (
                  <div key={key} className="flex items-start justify-between gap-4">
                    <div className="min-w-0 space-y-1">
                      <Label htmlFor={`storage-${key}`} help={help}>
                        {label}
                      </Label>
                      <StorageImpact storage={storage} performance={performance} />
                    </div>
                    <Switch
                      id={`storage-${key}`}
                      checked={settings.options[key]}
                      disabled={save.isPending}
                      onCheckedChange={(checked) => {
                        const options = { ...settings.options, [key]: checked }
                        setDraft({
                          ...settings,
                          options,
                          mode: options.all_fields ? 'full' : 'compact',
                        })
                        save.reset()
                      }}
                    />
                  </div>
                ))}
              </div>
              <div className="field max-w-xl">
                <Label
                  htmlFor="storage-fields"
                  help="Optional mapped fields to keep individually instead of enabling all fields. An empty selection retains only required fields. Newly added source fields are not selected automatically. The impact varies with the size and number of selected fields."
                >
                  Additional source fields
                </Label>
                <MultiSelect
                  id="storage-fields"
                  label="Additional source fields"
                  allLabel={
                    settings.options.all_fields
                      ? 'All mapped fields retained'
                      : 'Required fields only'
                  }
                  value={settings.options.all_fields ? [] : settings.options.additional_fields}
                  disabled={settings.options.all_fields || save.isPending}
                  options={(query.data?.available_fields ?? []).map((field) => ({
                    value: field,
                    label: field,
                  }))}
                  onChange={(additional_fields) => {
                    setDraft({ ...settings, options: { ...settings.options, additional_fields } })
                    save.reset()
                  }}
                />
                <StorageImpact storage="medium" performance="low" />
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
          )}
        </Disclosure>
        <StorageIndexesPanel />
        <StorageRetentionPanel />
        <RuntimeSettingsPanel section="history" />
        <Disclosure
          id="maintenance"
          title="Maintenance"
          open={maintenanceOpen}
          onOpenChange={setMaintenanceOpen}
          help="Optional: start over to reclaim space used by previously stored source data. Queued and paused imports are removed with their data."
        >
          <ConfirmAction
            variant="destructive"
            title="Reset all source data?"
            label="Reset source data"
            description="All imported torrents, remote catalog copies, source history, resume points and manual identifications will be removed. Source definitions, secrets, schedules, sharing permissions, film, series and book information, and debrid caches are retained. The next import starts from scratch using the saved storage settings. This cannot be undone."
            action={reset}
            trigger={
              <Button
                variant="destructive"
                disabled={Boolean(resetUnavailableReason)}
                aria-describedby={resetUnavailableReason ? 'source-data-reset-status' : undefined}
              >
                Reset all source data
              </Button>
            }
          />
          <p
            className={resetUnavailableReason ? 'help text-warning-foreground' : 'sr-only'}
            id="source-data-reset-status"
            role="status"
            aria-live="polite"
          >
            {resetUnavailableReason}
          </p>
        </Disclosure>
      </SettingsSections>
    </>
  )
}
