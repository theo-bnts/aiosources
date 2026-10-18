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
import type { Config, Status, Tracker } from '@/lib/aiosource-types'
import SharingPage from '@/pages/sharing'

type FormScope = 'metadata' | 'debrid' | 'trackers'
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
export default function AIOSourcePage({ section }: { section: 'metadata' | 'debrid' | 'stremio' }) {
  const location = useLocation()
  const [maintenanceOpen, setMaintenanceOpen] = useState(location.hash === '#maintenance')
  const query = useQuery({
    queryKey: ['aiosource'],
    queryFn: ({ signal }) => api<Status>('/aiosource', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
  })
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const [drafts, setDrafts] = useState<Partial<Record<FormScope, Draft>>>({})
  const [feedback, setFeedback] = useState<
    Partial<Record<FormScope, { error?: unknown; saved?: boolean }>>
  >({})
  const scope = section === 'metadata' ? 'metadata' : 'debrid'
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
      const saved = queryClient.getQueryData<Status>(['aiosource'])?.config
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
    mutationFn: (_form: FormScope) => api<Status>('/aiosource'),
    onSuccess: async (latest, form) => {
      await queryClient.cancelQueries({ queryKey: ['aiosource'] })
      queryClient.setQueryData(['aiosource'], latest)
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
    section === 'metadata' ? 'metadata settings' : 'debrid settings',
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
  const secret = (label: string, key: 'tmdb_secret' | 'alldebrid_secret' | 'torbox_secret') => (
    <SecretPicker
      inputId={key}
      label={label}
      value={config?.[key] ?? ''}
      onChange={(reference) => update(scope, { [key]: reference })}
      prefix={key.replace('_secret', '')}
      addLabel={`Add ${key === 'tmdb_secret' ? 'TMDB token' : key === 'alldebrid_secret' ? 'AllDebrid key' : 'TorBox key'}`}
      valueLabel={label}
      disabled={save.isPending}
    />
  )
  const number = (
    label: string,
    key: 'sync_minutes' | 'tmdb_requests_per_second' | 'cache_minutes' | 'response_seconds',
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
  const toggle = (label: string, key: 'auto_sync' | 'auto_match') => (
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
  const busy = status.syncing || status.matching || action.isPending
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
        {drafts[form] && (
          <Button
            type="button"
            variant="warning"
            disabled={save.isPending}
            onClick={() => {
              setDrafts((current) => {
                const next = { ...current }
                delete next[form]
                return next
              })
              clearFeedback(form)
            }}
          >
            Discard changes
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
      aria-label={section === 'metadata' ? 'Metadata configuration' : 'Debrid configuration'}
      onSubmit={(e) => {
        e.preventDefault()
        submit(scope)
      }}
    >
      <fieldset disabled={save.isPending} className="contents">
        <Panel
          title={section === 'metadata' ? 'Automatic updates & identification' : 'Debrid settings'}
        >
          <div className="grid gap-5 md:grid-cols-2">
            {section === 'metadata' ? (
              <>
                {secret('TMDB read access token', 'tmdb_secret')}
                {number('Information update interval (minutes)', 'sync_minutes', 5, 1440)}
                {number('TMDB requests per second', 'tmdb_requests_per_second', 5, 40)}
                {toggle('Automatically update film & series information', 'auto_sync')}
                {toggle('Automatically identify torrents', 'auto_match')}
              </>
            ) : (
              <>
                {secret('AllDebrid API key', 'alldebrid_secret')}
                {secret('TorBox API key', 'torbox_secret')}
                {number('Cache validity (minutes)', 'cache_minutes', 5, 43200)}
                {number('Stream response timeout (seconds)', 'response_seconds', 2, 60)}
              </>
            )}
          </div>
          {section === 'metadata' && (
            <p className="help">
              Enable both options and save a TMDB token to keep information and torrent
              identification up to date automatically. Identification checks for new or changed
              torrents every 30 seconds. Both tasks continue while this page is closed. The request
              limit applies to the next information update; TMDB rate limits also pause requests
              automatically.
            </p>
          )}
          {footer(scope)}
        </Panel>
      </fieldset>
    </form>
  )
  return (
    <div className="page">
      <ErrorNotice error={query.error ?? secrets.error ?? providers.error} />
      <ErrorNotice error={action.error} scope="action" />
      {section === 'metadata' && (
        <>
          {status.rebuild_required && (
            <p role="alert" className="notice notice-warning">
              The change history gap exceeds 14 days. Reset the metadata catalog, then synchronize
              again.
            </p>
          )}
          {settingsForm}
          <Disclosure title="Manual actions">
            {status.error && (
              <p role="status" className="notice notice-warning">
                {messageText(status.error)}{' '}
                <Link className="data-link" to="/metadata/history">
                  View history
                </Link>
              </p>
            )}
            <p className="help">
              Run a task now without changing its automatic settings. Stopping the current task
              preserves committed progress; disable its automatic option to stop future runs.
            </p>
            <div className="flex flex-wrap gap-3">
              <Button
                disabled={busy || !status.config.tmdb_secret || status.rebuild_required}
                onClick={() => action.mutate('sync')}
              >
                Download information updates
              </Button>
              {status.syncing && (
                <Button
                  variant="warning"
                  onClick={() => action.mutate('stop-sync')}
                  disabled={action.isPending}
                >
                  Stop information updates
                </Button>
              )}
            </div>
            <div className="flex flex-wrap gap-3">
              <Button
                disabled={busy || status.status.works === 0}
                onClick={() => action.mutate('match')}
              >
                Identify torrents
              </Button>
              {status.matching && (
                <Button
                  variant="warning"
                  onClick={() => action.mutate('stop-matching')}
                  disabled={action.isPending}
                >
                  Stop identification
                </Button>
              )}
            </div>
          </Disclosure>
        </>
      )}
      {section === 'stremio' && (
        <>
          <SharingPage
            format="stremio"
            services={[
              ...(config.alldebrid_secret ? [{ id: 'alldebrid', name: 'AllDebrid' }] : []),
              ...(config.torbox_secret ? [{ id: 'torbox', name: 'TorBox' }] : []),
              { id: 'direct', name: 'Direct torrents' },
            ]}
          />
        </>
      )}
      {section === 'debrid' && settingsForm}
      {section === 'debrid' && (
        <form
          aria-label="Private source playback"
          onSubmit={(e) => {
            e.preventDefault()
            submit('trackers')
          }}
        >
          <fieldset disabled={save.isPending} className="contents">
            <Panel title="Private source playback">
              <p className="help">
                For uncached private torrents, add the complete announce URL, including its passkey,
                in{' '}
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
                  <div key={p.id} className="space-y-3 border-t pt-4">
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
            </Panel>
          </fieldset>
        </form>
      )}
      {section === 'debrid' && (
        <Disclosure title="Manual actions">
          <Button
            disabled={action.isPending || !status.config.alldebrid_secret}
            onClick={() => action.mutate('retry-cleanup')}
          >
            Retry temporary cleanup
          </Button>
        </Disclosure>
      )}
      {section !== 'stremio' && (
        <Disclosure
          id="maintenance"
          title="Maintenance"
          open={maintenanceOpen}
          onOpenChange={setMaintenanceOpen}
        >
          {section === 'metadata' && (
            <>
              <p className="help">
                Rebuild information or identification results when needed. Each action explains what
                it removes before confirmation.
              </p>
              <div className="flex flex-wrap gap-3">
                <ConfirmAction
                  title="Reset metadata catalog?"
                  description="Downloaded film & series information, pending imports and title matches will be removed. Collected torrents and manual choices are retained; reviewed information must become available again before playback."
                  label="Reset metadata"
                  variant="destructive"
                  action={() => action.mutateAsync('reset-metadata')}
                  trigger={
                    <Button variant="destructive" disabled={busy}>
                      Reset metadata
                    </Button>
                  }
                />
                <ConfirmAction
                  title="Reset title matches?"
                  description="Automatic correspondences will be recomputed from local film & series information. Manual choices are retained. No TMDB download is needed."
                  label="Reset matches"
                  variant="destructive"
                  action={() => action.mutateAsync('reset-mapping')}
                  trigger={
                    <Button variant="destructive" disabled={busy}>
                      Reset matches
                    </Button>
                  }
                />
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
      )}
    </div>
  )
}
