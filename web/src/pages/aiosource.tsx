import { debridProviders, type DebridSecret } from '@/lib/debrid'
import { AdvancedSettings } from '@/components/advanced-settings'
import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { messageText } from '@/lib/format'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import { api, ApiError, queryClient, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import type { Items, SecretInfo, ProviderSummary } from '@/lib/types'
import { Loading, ErrorNotice, ConfirmAction } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { SecretPicker } from '@/components/secret-picker'
import { FormStatus } from '@/components/form-status'
import type { Config, Controls, Tracker } from '@/lib/aiosource-types'
import SharingPage from '@/pages/sharing'
import { metadataSections, type MetadataSection } from '@/lib/metadata-navigation'

type FormScope = MetadataSection | 'debrid' | 'trackers'
type Changes = Partial<Omit<Config, 'revision' | 'trackers'>> & {
  trackers?: Record<string, Partial<Tracker>>
}
type Draft = { base: Config; changes: Changes }

function applyChanges(base: Config, changes: Changes): Config {
  const { trackers, ...settings } = changes
  const value = { ...base, ...settings, trackers: { ...base.trackers } }
  for (const [id, patch] of Object.entries(trackers ?? {})) {
    value.trackers[id] = {
      ...(value.trackers[id] ?? { private: true, announce_secret: '' }),
      ...patch,
    }
  }
  return value
}
function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="section-card">
      <h2 className="section-title">{title}</h2>
      {children}
    </section>
  )
}
export default function AIOSourcePage({
  section,
}: {
  section: MetadataSection | 'debrid' | 'stremio'
}) {
  const location = useLocation()
  const usesSecrets = section === 'videos' || section === 'debrid'
  const usesProviders = section === 'debrid'
  const [maintenanceOpen, setMaintenanceOpen] = useState(location.hash === '#maintenance')
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
    enabled: usesSecrets,
  })
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
    enabled: usesProviders,
  })
  const [drafts, setDrafts] = useState<Partial<Record<FormScope, Draft>>>({})
  const [feedback, setFeedback] = useState<
    Partial<Record<FormScope, { error?: unknown; saved?: boolean }>>
  >({})
  const metadata = section === 'videos' || section === 'books' || section === 'identification'
  const scope = section === 'stremio' ? 'debrid' : section
  const draft = drafts[scope]
  const config = draft ? applyChanges(draft.base, draft.changes) : query.data?.config
  const trackersDraft = drafts.trackers
  const trackersConfig = trackersDraft
    ? applyChanges(trackersDraft.base, trackersDraft.changes)
    : query.data?.config
  const save = useMutation({
    mutationFn: ({ value }: { scope: FormScope; value: Config }) =>
      api('/aiosource/config', { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async (_, savedForm) => {
      await queryClient.invalidateQueries({ queryKey: ['aiosource'] })
      const saved = queryClient.getQueryData<Controls>(['aiosource', 'controls'])?.config
      setDrafts((current) => {
        const next = { ...current }
        delete next[savedForm.scope]
        // Rebase other local edits after our own successful save. Keep their
        // changed fields while adopting the new revision and untouched values.
        if (saved && saved.revision === savedForm.value.revision + 1) {
          for (const key of Object.keys(next) as FormScope[]) {
            next[key] = { ...next[key]!, base: saved }
          }
        }
        return next
      })
      setFeedback((current) => ({ ...current, [savedForm.scope]: { saved: true } }))
    },
    onError: (error, failedForm) => {
      setFeedback((current) => ({ ...current, [failedForm.scope]: { error } }))
      if (error instanceof ApiError && error.status === 409)
        void queryClient.invalidateQueries({ queryKey: ['aiosource'] })
    },
  })
  const review = useMutation({
    mutationFn: (_form: FormScope) => api<Controls>('/aiosource/controls'),
    onSuccess: async (latest, form) => {
      await queryClient.cancelQueries({ queryKey: ['aiosource'] })
      queryClient.setQueryData(['aiosource', 'controls'], latest)
      setDrafts((current) =>
        current[form] ? { ...current, [form]: { ...current[form], base: latest.config } } : current,
      )
      clearFeedback(form)
    },
  })
  const conflict = (form: FormScope) =>
    Boolean(
      drafts[form] &&
      (drafts[form].base.revision !== query.data?.config.revision ||
        (feedback[form]?.error instanceof ApiError && feedback[form]?.error.status === 409)),
    )
  useUnsavedChanges(
    Boolean(
      draft &&
      JSON.stringify(applyChanges(draft.base, draft.changes)) !== JSON.stringify(draft.base),
    ),
    metadata ? `${metadataSections[section].label.toLowerCase()} settings` : 'debrid settings',
    save.isPending && save.variables?.scope === scope,
  )
  useUnsavedChanges(
    Boolean(
      trackersDraft &&
      JSON.stringify(applyChanges(trackersDraft.base, trackersDraft.changes)) !==
        JSON.stringify(trackersDraft.base),
    ),
    'private source playback',
    save.isPending && save.variables?.scope === 'trackers',
  )
  const action = useMutation({
    mutationFn: (name: string) => api(`/aiosource/${name}`, { method: 'POST' }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['aiosource'] })
    },
  })
  const update = (form: FormScope, patch: Changes) => {
    if (!query.data) return
    clearFeedback(form)
    const base = query.data.config
    setDrafts((current) => {
      const previous = current[form]
      const changes = { ...previous?.changes, ...patch }
      if (patch.trackers) {
        changes.trackers = { ...previous?.changes.trackers }
        for (const [id, value] of Object.entries(patch.trackers)) {
          changes.trackers[id] = { ...changes.trackers[id], ...value }
        }
      }
      const draft = { base: previous?.base ?? base, changes }
      const next = { ...current }
      if (JSON.stringify(applyChanges(draft.base, changes)) === JSON.stringify(draft.base)) {
        delete next[form]
      } else {
        next[form] = draft
      }
      return next
    })
  }
  const clearFeedback = (form: FormScope) => {
    setFeedback((current) => {
      const next = { ...current }
      delete next[form]
      return next
    })
  }
  const secret = (label: string, key: 'tmdb_secret' | DebridSecret) => (
    <SecretPicker
      inputId={key}
      label={label}
      value={config?.[key] ?? ''}
      onChange={(reference) => update(scope, { [key]: reference })}
      prefix={key.replace('_secret', '')}
      addLabel={`Add ${key === 'tmdb_secret' ? 'TMDB token' : `${debridProviders.find((provider) => provider.secret === key)?.name} key`}`}
      valueLabel={label}
      disabled={save.isPending}
    />
  )
  const number = (
    label: string,
    key:
      | 'sync_minutes'
      | 'match_seconds'
      | 'tmdb_requests_per_second'
      | 'cache_minutes'
      | 'response_seconds'
      | 'openlibrary_minutes',
    min: number,
    max: number,
  ) => (
    <div className="field">
      <Label htmlFor={key}>{label}</Label>
      <Input
        id={key}
        type="number"
        disabled={save.isPending}
        required
        min={min}
        max={max}
        value={config?.[key] ?? ''}
        onChange={(e) => update(scope, { [key]: e.target.valueAsNumber })}
      />
    </div>
  )
  const toggle = (label: string, key: 'auto_sync' | 'auto_match' | 'openlibrary_enabled') => (
    <label className="flex items-center gap-3 text-sm">
      <Switch
        aria-label={label}
        checked={config?.[key] ?? false}
        disabled={save.isPending}
        onCheckedChange={(checked) => update(scope, { [key]: checked })}
      />
      {label}
    </label>
  )
  if (query.isPending)
    return (
      <div className="page">
        <Loading />
      </div>
    )
  if (!config || !query.data)
    return (
      <div className="page">
        <ErrorNotice error={query.error} />
      </div>
    )
  const status = query.data
  const busy = status.syncing || status.matching || status.books?.running || action.isPending
  const submit = (form: FormScope) => {
    const local = drafts[form]
    if (local && !save.isPending && !review.isPending && !conflict(form)) {
      save.mutate({ scope: form, value: applyChanges(local.base, local.changes) })
    }
  }
  const footer = (form: FormScope) => (
    <>
      <ErrorNotice error={feedback[form]?.error} />
      {review.variables === form && <ErrorNotice error={review.error} />}
      {conflict(form) && (
        <p role="status" className="text-sm text-muted-foreground">
          These settings changed elsewhere. Load the latest settings to review them; your edits will
          be kept.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-4 border-t pt-5">
        <Button
          type="submit"
          disabled={!drafts[form] || save.isPending || review.isPending || conflict(form)}
        >
          {save.isPending && save.variables?.scope === form ? 'Saving…' : 'Save'}
        </Button>
        {conflict(form) && (
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending || review.isPending}
            onClick={() => review.mutate(form)}
          >
            Load latest settings
          </Button>
        )}
        <FormStatus
          dirty={Boolean(drafts[form])}
          saving={save.isPending && save.variables?.scope === form}
          saved={feedback[form]?.saved}
        />
      </div>
    </>
  )
  const settingsForm = (
    <form
      aria-label={
        metadata ? `${metadataSections[section].label} configuration` : 'Debrid configuration'
      }
      onSubmit={(e) => {
        e.preventDefault()
        submit(scope)
      }}
    >
      <fieldset disabled={save.isPending} className="contents">
        <Panel
          title={
            metadata
              ? section === 'identification'
                ? 'Automatic identification'
                : 'Information settings'
              : 'Debrid settings'
          }
        >
          {section === 'videos' && (
            <div className="grid gap-5 md:grid-cols-2">
              {secret('TMDB read access token', 'tmdb_secret')}
              {number('TMDB requests per second', 'tmdb_requests_per_second', 5, 40)}
            </div>
          )}
          {section === 'books' && (
            <p className="help">
              Open Library supplies book and audiobook information without an API key. The first
              catalog import can take hours and needs substantial storage. Later updates follow
              recent changes, with a fresh export when recovery is needed. Cover images stay remote.
            </p>
          )}
          {section === 'debrid' && (
            <div className="grid gap-5 md:grid-cols-2">
              {debridProviders.map((provider) => (
                <div key={provider.id} className="min-w-0 space-y-2">
                  {secret(`${provider.name} API key`, provider.secret)}
                  {'hint' in provider && <p className="help">{provider.hint}</p>}
                </div>
              ))}
              {number('Cache validity (minutes)', 'cache_minutes', 5, 43200)}
            </div>
          )}
          {metadata && (
            <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
              <legend className="pr-3 text-sm font-medium">Schedules</legend>
              <div className="grid gap-5 md:grid-cols-2">
                {section === 'videos' && (
                  <div className="space-y-4">
                    {toggle('Automatically update film & series information', 'auto_sync')}
                    {number('Information update interval (minutes)', 'sync_minutes', 5, 1440)}
                  </div>
                )}
                {section === 'books' && (
                  <div className="space-y-4">
                    {toggle('Automatically update book information', 'openlibrary_enabled')}
                    {number(
                      'Book information update interval (minutes)',
                      'openlibrary_minutes',
                      5,
                      1440,
                    )}
                  </div>
                )}
                {section === 'identification' && (
                  <div className="space-y-4">
                    {toggle('Automatically identify torrents', 'auto_match')}
                    {number('Identification interval (seconds)', 'match_seconds', 5, 86400)}
                  </div>
                )}
              </div>
              {section === 'identification' && (
                <p className="help">
                  Identification waits for this interval after each attempt. Pending work and timing
                  survive restarts. These settings apply to videos and books. Manual actions run
                  immediately.
                </p>
              )}
            </fieldset>
          )}
          {footer(scope)}
        </Panel>
      </fieldset>
    </form>
  )
  const maintenance = (
    <Disclosure
      id="maintenance"
      title="Maintenance"
      open={maintenanceOpen}
      onOpenChange={setMaintenanceOpen}
    >
      {metadata && (
        <>
          <p className="help">
            Rebuild information or identification results when needed. Each action explains what it
            removes before confirmation.
          </p>
          <div className="flex flex-wrap gap-3">
            {section === 'videos' && (
              <ConfirmAction
                title="Reset video information?"
                description="Film and series information, pending video updates and automatic video matches will be removed. Torrent records, manual choices and book information are retained. Automatic video updates can import the catalog again."
                label="Reset video information"
                variant="destructive"
                action={() => action.mutateAsync('reset-videos')}
                trigger={
                  <Button variant="destructive" disabled={busy}>
                    Reset video information
                  </Button>
                }
              />
            )}
            {section === 'books' && (
              <ConfirmAction
                title="Reset book information?"
                description="Open Library information and automatic book matches will be removed. Torrent records, manual choices and film & series information are retained. Automatic book updates can import the catalog again."
                label="Reset book information"
                variant="destructive"
                action={() => action.mutateAsync('reset-books')}
                trigger={
                  <Button variant="destructive" disabled={busy}>
                    Reset book information
                  </Button>
                }
              />
            )}
            {section === 'identification' && (
              <ConfirmAction
                title="Reset title matches?"
                description="Automatic correspondences will be recomputed from the local information catalog. Manual choices are retained. No metadata download is needed."
                label="Reset matches"
                variant="destructive"
                action={() => action.mutateAsync('reset-mapping')}
                trigger={
                  <Button variant="destructive" disabled={busy}>
                    Reset matches
                  </Button>
                }
              />
            )}
          </div>
        </>
      )}
      {section === 'debrid' && (
        <>
          <p className="help">
            Available, absent and unknown results stay distinct. Cache observations and retries
            survive restart and are isolated by provider account.
          </p>
          <div className="flex flex-wrap items-center gap-4">
            <ConfirmAction
              title="Reset cache observations?"
              description="Availability will be checked again on the next search. Temporary magnet ownership records are preserved."
              label="Reset cache"
              variant="destructive"
              action={() => action.mutateAsync('reset-cache')}
              trigger={
                <Button variant="destructive" disabled={action.isPending}>
                  Reset cache
                </Button>
              }
            />
          </div>
        </>
      )}
    </Disclosure>
  )
  return (
    <div className="page">
      <ErrorNotice
        error={
          query.error ??
          (usesSecrets ? secrets.error : null) ??
          (usesProviders ? providers.error : null)
        }
      />
      <ErrorNotice error={action.error} scope="action" />
      {metadata && (
        <>
          {settingsForm}
          <AdvancedSettings>
            {section === 'videos' && <RuntimeSettingsPanel section="metadata" />}
            {(section === 'videos' || section === 'books') && (
              <RuntimeSettingsPanel
                section="metadata_retries"
                scopeHelp="These settings apply to both video and book information updates."
              />
            )}
            {section === 'identification' && (
              <>
                <RuntimeSettingsPanel section="identification" />
                <RuntimeSettingsPanel section="matching" />
              </>
            )}
            <Disclosure title="Manual actions">
              {section === 'videos' && status.recovery_required && (
                <p role="status" className="notice notice-warning">
                  The TMDB change history could not cover the interruption. Film & series
                  information will be refreshed progressively. Existing data, books and manual
                  choices are retained.{' '}
                  {status.config.auto_sync
                    ? 'Automatic recovery continues from saved progress.'
                    : 'Enable automatic video updates, or run Update film & series information to continue.'}
                </p>
              )}
              {section === 'books' && Boolean(status.books?.skipped) && (
                <p role="status" className="notice notice-warning">
                  {status.books!.skipped.toLocaleString()} unreadable or oversized book records were
                  skipped.{' '}
                  <Link className="data-link" to="/metadata/books/history">
                    View history
                  </Link>
                </p>
              )}
              {section === 'books' && status.books?.running && (
                <p role="status" className="help">
                  Updating book information. Progress is saved automatically and continues after an
                  interruption.
                </p>
              )}
              {section === 'books' && status.books?.needs_export && (
                <p role="status" className="notice notice-warning">
                  Change continuity was lost. A newer Open Library export is needed to cover the
                  missing period. Existing data is retained.{' '}
                  {status.config.openlibrary_enabled
                    ? 'Recent updates continue while automatic recovery waits for that export.'
                    : 'Enable automatic book updates, or run Update book information to retry manually.'}
                </p>
              )}
              {Boolean(
                section === 'videos'
                  ? status.status.sync_error
                  : section === 'books'
                    ? status.books?.error
                    : status.matching_error,
              ) && (
                <p role="status" className="notice notice-warning">
                  {messageText(
                    section === 'videos'
                      ? status.status.sync_error
                      : section === 'books'
                        ? status.books?.error || ''
                        : status.matching_error || '',
                  )}{' '}
                  <Link className="data-link" to={`${metadataSections[section].path}/history`}>
                    View history
                  </Link>
                </p>
              )}
              <p className="help">
                Run a task now without changing its automatic settings. Stopping the current task
                preserves committed progress; disable its automatic option to stop future runs.
              </p>
              {draft && <p className="help">Save your changes before starting a run.</p>}
              <div className="flex flex-wrap gap-3">
                {section === 'videos' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.syncing || action.isPending || draft || !status.config.tmdb_secret,
                      )}
                      onClick={() => action.mutate('sync')}
                    >
                      Update film & series information
                    </Button>
                    {status.syncing && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-sync')}
                      >
                        Stop film & series information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'books' && (
                  <>
                    <Button
                      disabled={Boolean(status.books?.running || action.isPending || draft)}
                      onClick={() => action.mutate('sync-books')}
                    >
                      Update book information
                    </Button>
                    {status.books?.running && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-books')}
                      >
                        Stop book information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'identification' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.matching ||
                        action.isPending ||
                        draft ||
                        !status.status.has_information,
                      )}
                      onClick={() => action.mutate('match')}
                    >
                      Identify torrents
                    </Button>
                    {status.matching && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-matching')}
                      >
                        Stop identification
                      </Button>
                    )}
                  </>
                )}
              </div>
            </Disclosure>
            {maintenance}
          </AdvancedSettings>
        </>
      )}
      {section === 'stremio' && (
        <>
          <SharingPage
            format="stremio"
            services={[
              ...debridProviders.filter((provider) => config[provider.secret]),
              { id: 'direct', name: 'Direct torrents' },
            ]}
          />
        </>
      )}
      {section === 'debrid' && (
        <>
          {settingsForm}
          <AdvancedSettings>
            <Disclosure title="Private source playback">
              <form
                aria-label="Private source playback"
                onSubmit={(e) => {
                  e.preventDefault()
                  submit('trackers')
                }}
              >
                <fieldset disabled={save.isPending} className="space-y-4">
                  <p className="help">
                    For uncached private torrents, add the complete announce URL, including its
                    passkey, in{' '}
                    <Link className="data-link" to="/security/settings#secrets">
                      Secrets
                    </Link>
                    , or directly below, then save the playback settings. The URL is used exactly as
                    supplied by the tracker, whatever the position of the passkey.
                  </p>
                  {providers.data?.items?.length === 0 && (
                    <p className="text-sm text-muted-foreground">
                      Add your own sources first. No tracker is bundled.
                    </p>
                  )}
                  {providers.data?.items?.map((p) => {
                    const tracker = trackersConfig?.trackers[p.id] ?? {
                      private: true,
                      announce_secret: '',
                    }
                    const change = (patch: Partial<Tracker>) =>
                      update('trackers', { trackers: { [p.id]: patch } })
                    return (
                      <div key={p.id} id={`tracker-${p.id}`} className="space-y-3 border-t pt-4">
                        <h3 className="font-medium">{p.name}</h3>
                        <label className="flex items-center gap-3 text-sm">
                          <Switch
                            aria-label="Private source"
                            checked={tracker.private}
                            onCheckedChange={(checked) => change({ private: checked })}
                          />
                          Private source
                        </label>
                        <SecretPicker
                          inputId={`announce-${p.id}`}
                          label="Announce URL secret"
                          value={tracker.announce_secret}
                          onChange={(reference) => change({ announce_secret: reference })}
                          prefix={`announce_${p.id}`}
                          addLabel="Add announce URL"
                          valueLabel="Complete announce URL"
                          disabled={save.isPending || !tracker.private}
                        />
                      </div>
                    )
                  })}
                  {footer('trackers')}
                </fieldset>
              </form>
            </Disclosure>
            <RuntimeSettingsPanel section="debrid" />
            <RuntimeSettingsPanel section="debrid_retries" />
            <Disclosure title="Manual actions">
              <Button
                disabled={action.isPending || !status.config.alldebrid_secret}
                onClick={() => action.mutate('retry-cleanup')}
              >
                Retry temporary cleanup
              </Button>
            </Disclosure>
            {maintenance}
          </AdvancedSettings>
        </>
      )}
    </div>
  )
}
