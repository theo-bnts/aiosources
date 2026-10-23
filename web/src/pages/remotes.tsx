import { useSettingsEditor } from '@/lib/settings-navigation'
import { toggleProvider } from '@/lib/provider-state'
import { SecretPicker } from '@/components/secret-picker'
import { Statistic } from '@/components/statistic'
import { useNotification } from '@/lib/notifications'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  PlayIcon,
  PlusIcon,
  CloudArrowDownIcon,
  MagnifyingGlassIcon,
  PencilSimpleIcon,
  TrashIcon,
} from '@phosphor-icons/react'
import { api, ApiError, ConnectionError, refreshData, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import type { RemoteCatalog } from '@/lib/catalog-types'
import type { Items, ProviderDocument, Validation } from '@/lib/types'
import { mergeRemoteDraft, remoteDraft, remoteJSON, validateRemoteURL } from '@/lib/remote-config'
import type { RemoteDraft } from '@/lib/remote-config'
import { messageText, useDate } from '@/lib/format'
import { useListViewState } from '@/lib/detail-navigation'
import { SourceFilters, sourceMatches, useSourceFilters } from '@/components/source-filters'
import { FormStatus } from '@/components/form-status'
import { CollectDialog } from '@/components/collect-dialog'
import { SourceTorrentStats, useSourceTorrentStats } from '@/components/source-torrent-stats'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { ConfirmAction, Empty, ErrorNotice, Loading, RunStatusBadge } from '@/components/common'

function safeConfigurationError(error: unknown) {
  if (error instanceof ConnectionError) return error
  if (!(error instanceof ApiError))
    return new Error(
      'The remote configuration could not be saved. Your draft is preserved; check the settings and try again.',
    )
  if (error.status === 409)
    return new ApiError(409, 'The source changed on the server. Your draft has been preserved.')
  if (error.status === 422 || error.status === 400)
    return new ApiError(
      error.status,
      'The remote definition was rejected. Check its HTTPS URL and vault reference.',
    )
  if (error.status === 401 || error.status === 403)
    return new ApiError(error.status, 'Your security session needs to be refreshed before saving.')
  return new ApiError(
    error.status,
    'The server could not confirm the save. Your draft is preserved. Reload the current source before retrying.',
  )
}

function useRemotes() {
  return useQuery({
    queryKey: ['remotes'],
    queryFn: ({ signal }) => api<Items<RemoteCatalog>>('/remotes', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
}

export function RemoteStatisticsPage() {
  const remotes = useRemotes()
  const date = useDate()
  const items = remotes.data?.items ?? []
  const counts = useSourceTorrentStats(items.length > 0)
  const totals = items.reduce(
    (total, remote) => {
      const count = counts.bySource.get(remote.id)
      total.imported += count?.imported ?? 0
      total.awaiting_import += count?.pending_import?.torrents ?? 0
      return total
    },
    { imported: 0, awaiting_import: 0 },
  )
  return (
    <div className="page">
      <ErrorNotice error={remotes.error} retry={() => void remotes.refetch()} />
      <ErrorNotice error={counts.error} retry={() => void counts.refetch()} />
      {remotes.isPending ? (
        <Loading />
      ) : (
        remotes.data && (
          <>
            <dl className="grid grid-cols-2 gap-3 xl:grid-cols-4">
              <Statistic
                label="Imported torrents"
                value={counts.data || !items.length ? totals.imported : 'Not available'}
                loadingLabel={items.length > 0 && counts.isPending ? 'Loading counts…' : undefined}
              />
              <Statistic
                label="Awaiting import completion"
                value={counts.data || !items.length ? totals.awaiting_import : 'Not available'}
                loadingLabel={items.length > 0 && counts.isPending ? 'Loading counts…' : undefined}
                to="/remotes/history"
              />
              <Statistic
                label="Enabled catalogs"
                value={items.filter((item) => item.enabled).length}
              />
              <Statistic
                label="Synced catalogs"
                value={items.filter((item) => item.has_checkpoint).length}
              />
            </dl>
            {items.length ? (
              <div className="grid items-start gap-5 md:grid-cols-2 xl:grid-cols-3">
                {items.map((remote) => (
                  <section
                    key={remote.id}
                    className="section-card"
                    aria-label={`Remote catalog ${remote.name || remote.id}`}
                  >
                    <h2 className="section-title">{remote.name || remote.id}</h2>
                    <SourceTorrentStats
                      source={remote.name || remote.id}
                      counts={counts.bySource.get(remote.id)}
                      unavailable={counts.isError}
                      totals={counts.totals}
                    />
                    <p className="help">
                      Last successful sync:{' '}
                      {remote.last_synced_at ? date(remote.last_synced_at) : 'Never completed'}
                    </p>
                  </section>
                ))}
              </div>
            ) : (
              !remotes.isError && (
                <Empty
                  title="No remote catalogs"
                  description="Add a catalog in Settings to import a shared library."
                />
              )
            )}
          </>
        )
      )}
    </div>
  )
}

export default function RemotesPage() {
  const listState = useListViewState()
  const filters = useSourceFilters()
  const date = useDate()
  const returnFocus = useRef<HTMLElement | null>(null)
  const [editing, setEditing] = useState<RemoteCatalog | 'new' | null>(null)
  const [saving, setSaving] = useState(false)
  const notify = useNotification()
  const remotes = useRemotes()
  useSettingsEditor(
    'remote',
    remotes.data ? (remotes.data.items ?? []) : undefined,
    (remote) => openEditor(remote),
    () => setEditing(null),
  )
  const items = remotes.data?.items ?? []
  const visible = items.filter(
    (remote) =>
      sourceMatches(filters.query, remote.name, remote.id) &&
      (!filters.state ||
        (filters.state === 'enabled' && remote.enabled) ||
        (filters.state === 'disabled' && !remote.enabled) ||
        (filters.state === 'invalid' && !remote.valid)),
  )
  const filtered = Boolean(filters.query || filters.state)
  function openEditor(remote: RemoteCatalog | 'new') {
    returnFocus.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
    setEditing(remote)
  }
  const toggle = useMutation({ mutationFn: toggleProvider, onSuccess: refreshData })
  return (
    <div className="page">
      <div className="flex justify-end">
        <Button onClick={() => openEditor('new')}>
          <PlusIcon />
          Add remote catalog
        </Button>
      </div>
      {(items.length > 0 || filtered) && (
        <SourceFilters
          filters={filters}
          states={[
            { value: 'enabled', label: 'Enabled' },
            { value: 'disabled', label: 'Disabled' },
            { value: 'invalid', label: 'Needs fixing' },
          ]}
          count={visible.length}
          total={items.length}
        />
      )}
      <ErrorNotice
        error={toggle.error}
        retry={() => {
          void remotes.refetch().then(() => toggle.reset())
        }}
      />
      <ErrorNotice error={remotes.error} retry={() => void remotes.refetch()} />
      {remotes.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="space-y-4">
          {visible.map((remote) => {
            const active =
              remote.last_run?.status === 'queued' ||
              remote.last_run?.status === 'running' ||
              remote.last_run?.status === 'paused'
            return (
              <section
                key={remote.id}
                className="section-card"
                aria-label={`Remote catalog ${remote.name || remote.id}`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <h2 className="section-title break-words">{remote.name || remote.id}</h2>
                    <p className="mono mt-1 break-all text-muted-foreground">{remote.url}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Switch
                      checked={remote.enabled}
                      aria-label={`${remote.enabled ? 'Disable' : 'Enable'} ${remote.name || remote.id}`}
                      disabled={toggle.isPending || (!remote.enabled && !remote.valid)}
                      onCheckedChange={() => toggle.mutate(remote)}
                    />
                    {!remote.valid && <Badge variant="destructive">Invalid configuration</Badge>}
                  </div>
                </div>
                <dl className="grid gap-4 text-sm md:grid-cols-2">
                  <div>
                    <dt className="text-muted-foreground">Schedule</dt>
                    <dd className="mt-1">
                      {remote.sync.enabled
                        ? `Every ${remote.sync.interval_minutes} minutes`
                        : 'Manual imports only'}
                    </dd>
                    {remote.sync_error && (
                      <p className="help mt-1 text-destructive">{messageText(remote.sync_error)}</p>
                    )}
                  </div>
                  <div>
                    <dt className="text-muted-foreground">Last run</dt>
                    <dd className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-2">
                      {remote.last_run ? (
                        <Link
                          to={`/remotes/history/${encodeURIComponent(remote.last_run.id)}`}
                          state={listState}
                        >
                          <RunStatusBadge run={remote.last_run} />
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">No runs</span>
                      )}
                      {remote.enabled &&
                        remote.valid &&
                        remote.sync.enabled &&
                        remote.next_sync_at &&
                        ['waiting', 'paused'].includes(remote.sync_state) &&
                        !['queued', 'running'].includes(remote.last_run?.status ?? '') && (
                          <span className="text-muted-foreground">
                            Next: {date(remote.next_sync_at)}
                          </span>
                        )}
                    </dd>
                  </div>
                </dl>
                {remote.last_run?.error && (
                  <p role="status" className="notice notice-error break-words">
                    Latest run error: {messageText(remote.last_run.error)}
                  </p>
                )}
                {remote.last_run?.status === 'paused' && (
                  <p className="help">
                    {remote.enabled && remote.sync.enabled && remote.sync_state === 'paused'
                      ? 'This import will resume at the next automatic sync, keeping its saved progress. You can also resume it now from run details.'
                      : 'A paused run has unpublished progress. Resume or cancel it from run details before starting another sync.'}
                  </p>
                )}
                <div className="toolbar">
                  <CollectDialog
                    providerId={remote.id}
                    remote
                    trigger={
                      <Button size="sm" disabled={!remote.valid || !remote.enabled || active}>
                        <PlayIcon />
                        Run manual import
                      </Button>
                    }
                  />
                  <Button size="sm" variant="outline" onClick={() => openEditor(remote)}>
                    <PencilSimpleIcon />
                    Edit
                  </Button>
                  <Button size="sm" variant="outline" asChild>
                    <Link
                      to={`/torrents?provider=${encodeURIComponent(remote.id)}`}
                      state={listState}
                    >
                      <MagnifyingGlassIcon />
                      View imported data
                    </Link>
                  </Button>
                  <ConfirmAction
                    title={`Delete remote catalog “${remote.name || remote.id}”?`}
                    description="The connection, imported torrents, synchronization history, saved progress and local identifications will be permanently deleted. Secrets, shared metadata and debrid caches are kept. This cannot be undone."
                    label="Delete remote catalog"
                    variant="destructive"
                    trigger={
                      <Button
                        size="sm"
                        variant="destructive"
                        disabled={['queued', 'running'].includes(remote.last_run?.status ?? '')}
                      >
                        <TrashIcon />
                        Delete
                      </Button>
                    }
                    action={async () => {
                      await api<void>(`/providers/${encodeURIComponent(remote.id)}`, {
                        method: 'DELETE',
                        body: JSON.stringify({ revision: remote.revision }),
                      })
                      await refreshData()
                    }}
                  />
                </div>
              </section>
            )
          })}
        </div>
      ) : (
        !remotes.isError && (
          <Empty
            icon={filtered ? MagnifyingGlassIcon : CloudArrowDownIcon}
            title={filtered ? 'No remote catalogs match these filters' : 'No remote catalogs'}
            description={
              filtered
                ? 'Clear the filters to see your other sources.'
                : 'Add a shared HTTPS catalog URL and select or store its password in Secrets.'
            }
          />
        )
      )}
      <Dialog
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open && !saving) setEditing(null)
        }}
      >
        <DialogContent
          data-settings-editor="remote"
          data-settings-entity={editing === 'new' ? 'new' : editing?.id}
          className="sm:max-w-2xl"
          showCloseButton={!saving}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            returnFocus.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {editing === 'new' ? 'Add remote catalog' : 'Edit remote catalog'}
            </DialogTitle>
            <DialogDescription>
              Connect to a shared catalog. Published torrent fields are imported; film and series
              identification is performed locally.
            </DialogDescription>
          </DialogHeader>
          {editing && (
            <RemoteDocument
              key={editing === 'new' ? 'new' : editing.id}
              remote={editing === 'new' ? undefined : editing}
              onSaving={setSaving}
              onSaved={() => {
                setEditing(null)
                notify('Remote catalog settings saved.')
              }}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

function RemoteDocument({
  remote,
  onSaving,
  onSaved,
}: {
  remote?: RemoteCatalog
  onSaving: (value: boolean) => void
  onSaved: () => void
}) {
  const document = useQuery({
    queryKey: ['remote-document', remote?.id],
    queryFn: ({ signal }) =>
      api<ProviderDocument>(`/providers/${encodeURIComponent(remote!.id)}`, { signal }),
    enabled: Boolean(remote),
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })
  if (remote && document.isPending) return <Loading label="Loading remote definition" />
  if (remote && !document.data)
    return <ErrorNotice error={document.error} retry={() => void document.refetch()} />
  return <RemoteForm document={document.data} onSaving={onSaving} onSaved={onSaved} />
}

function RemoteForm({
  document,
  onSaving,
  onSaved,
}: {
  document?: ProviderDocument
  onSaving: (value: boolean) => void
  onSaved: () => void
}) {
  const [original, setOriginal] = useState(document)
  const [draft, setDraft] = useState(() => remoteDraft(document))
  const [reloaded, setReloaded] = useState(false)
  const [localError, setLocalError] = useState<Error | null>(null)
  function change<K extends keyof RemoteDraft>(key: K, value: RemoteDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }))
  }
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      const reference = draft.secretRef.trim()
      const json = remoteJSON(original, draft, reference)
      let validation: Validation
      try {
        validation = await api<Validation>('/providers/validate', {
          method: 'POST',
          body: JSON.stringify({ json }),
        })
      } catch (error) {
        throw safeConfigurationError(error)
      }
      if (!validation.valid)
        throw new ApiError(
          422,
          'The remote definition is invalid. Review the settings before saving.',
          validation.issues ?? [],
        )
      let result: ProviderDocument
      try {
        result = await api<ProviderDocument>(
          `/providers/${encodeURIComponent(original?.provider.id ?? draft.id.trim())}`,
          { method: 'PUT', body: JSON.stringify({ json, revision: original?.revision ?? '' }) },
        )
      } catch (error) {
        throw safeConfigurationError(error)
      }
      setOriginal(result)
      await refreshData()
      onSaved()
    },
    onMutate: () => onSaving(true),
    onSettled: () => onSaving(false),
  })
  const reload = useMutation({
    mutationFn: () =>
      api<ProviderDocument>(
        `/providers/${encodeURIComponent(original?.provider.id ?? draft.id.trim())}`,
      ),
    onSuccess: (latest) => {
      setDraft((current) => mergeRemoteDraft(current, remoteDraft(original), remoteDraft(latest)))
      setOriginal(latest)
      setReloaded(true)
      save.reset()
    },
  })
  const conflict = save.error instanceof ApiError && save.error.status === 409
  const busy = save.isPending || reload.isPending
  const dirty = JSON.stringify(draft) !== JSON.stringify(remoteDraft(original))
  useUnsavedChanges(dirty, 'remote catalog', busy)
  return (
    <form
      className="space-y-5"
      autoComplete="off"
      onSubmit={(event) => {
        event.preventDefault()
        if (busy || conflict || (original && !dirty)) return
        setLocalError(null)
        try {
          validateRemoteURL(draft.url.trim())
          if (!draft.secretRef.trim()) throw new Error('Select or add a sharing password.')
          save.mutate()
        } catch (error) {
          setLocalError(error instanceof Error ? error : new Error('Review the remote settings.'))
        }
      }}
    >
      <fieldset className="space-y-5" disabled={busy}>
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <div className="field">
            <Label htmlFor="remote-id">Local source identifier</Label>
            <Input
              id="remote-id"
              required
              pattern="[A-Za-z0-9][A-Za-z0-9_-]*"
              maxLength={128}
              readOnly={Boolean(original)}
              value={draft.id}
              onChange={(event) => change('id', event.target.value)}
            />
            <p className="help">Unique locally. Imported data stays in this source’s namespace.</p>
          </div>
          <div className="field">
            <Label htmlFor="remote-name">Display name</Label>
            <Input
              id="remote-name"
              required
              maxLength={200}
              value={draft.name}
              onChange={(event) => change('name', event.target.value)}
            />
          </div>
        </div>
        <div className="field">
          <Label htmlFor="remote-url">HTTPS catalog URL</Label>
          <Input
            id="remote-url"
            type="url"
            required
            value={draft.url}
            onChange={(event) => change('url', event.target.value)}
            placeholder="https://catalog.example/api/catalogs/share-id"
            autoComplete="off"
            spellCheck={false}
            aria-describedby="remote-url-help"
          />
          <p id="remote-url-help" className="help">
            Use the owner’s HTTPS catalog URL without credentials or query parameters.
          </p>
          {original && draft.url.trim() !== original.provider.url && (
            <p className="help">
              Changing the URL starts a fresh snapshot on the next sync. The current copy remains
              until that sync succeeds.
            </p>
          )}
        </div>
        <SecretPicker
          label="Sharing password"
          value={draft.secretRef}
          onChange={(reference) => change('secretRef', reference)}
          prefix="remote_catalog"
          addLabel="Add sharing password"
          valueLabel="Sharing password"
          emptyLabel="Select a secret"
          required
          disabled={busy}
        />
        <div className="flex items-start justify-between gap-4 rounded-md border border-border p-3">
          <div>
            <Label htmlFor="remote-enabled">Connection enabled</Label>
            <p className="help mt-1">
              Allows manual and automatic imports. Disabling does not cancel an existing run.
            </p>
          </div>
          <Switch
            id="remote-enabled"
            checked={draft.enabled}
            onCheckedChange={(value) => change('enabled', value)}
          />
        </div>
        <section
          className="space-y-4 rounded-md border border-border p-4"
          aria-label="Automatic synchronization"
        >
          <div className="flex items-start justify-between gap-4">
            <div>
              <Label htmlFor="remote-auto-sync">Automatic synchronization</Label>
              <p className="help mt-1">
                The server keeps this local copy up to date even when the console is closed.
              </p>
            </div>
            <Switch
              id="remote-auto-sync"
              checked={draft.autoSync}
              onCheckedChange={(value) => change('autoSync', value)}
            />
          </div>
          <div className="field">
            <Label htmlFor="remote-sync-minutes">Synchronization interval (minutes)</Label>
            <Input
              id="remote-sync-minutes"
              type="number"
              min={1}
              max={525600}
              step={1}
              required
              value={draft.intervalMinutes}
              onChange={(event) => change('intervalMinutes', event.target.value)}
            />
          </div>
          <p className="help">
            The first sync imports a snapshot. Later syncs request changes from the saved
            checkpoint. Incomplete imports keep the current published copy intact.
          </p>
        </section>
      </fieldset>
      <p className="notice">
        Saving waits for the next automatic sync. Use Run manual import for an immediate import.
        Failed or incomplete runs leave the published copy and checkpoint unchanged.
      </p>
      {original && (
        <p className="help">
          Editing a source with a paused scheduled run can block automatic continuation. Resume or
          cancel the old run from history. Imported fields cannot be changed in this form.
        </p>
      )}
      <ErrorNotice error={localError} />
      <ErrorNotice error={save.error} />
      <ErrorNotice error={reload.error} />
      {conflict && (
        <div className="notice space-y-3">
          <p>
            Your draft is preserved. Reload the current source to retain its comments and unrelated
            settings. Only your changed form values will be reapplied; review them before saving.
          </p>
          <Button type="button" variant="outline" disabled={busy} onClick={() => reload.mutate()}>
            Reload source, keep edits
          </Button>
        </div>
      )}
      {reloaded && !conflict && (
        <p role="status" className="notice">
          Latest source loaded. Your changed fields are preserved; untouched fields reflect the
          server. Review before saving again.
        </p>
      )}
      <FormStatus dirty={dirty} saving={save.isPending} />
      <DialogFooter>
        <Button
          type="submit"
          disabled={
            busy ||
            conflict ||
            Boolean(original && !dirty) ||
            !draft.name.trim() ||
            !draft.id.trim()
          }
        >
          {save.isPending ? 'Saving…' : original ? 'Save remote' : 'Create remote catalog'}
        </Button>
      </DialogFooter>
    </form>
  )
}
