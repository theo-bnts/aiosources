import { SourceFilter } from '@/components/source-filter'
import { Disclosure } from '@/components/ui/disclosure'
import { TorznabSetup } from '@/components/torznab-setup'
import {
  StremioConnectionFilters,
  defaultStremioPreferences,
  stremioPreferenceSummary,
} from '@/components/stremio-connection-filters'
import { resolvedLanguage } from '@/lib/language-preferences'
import { ResultCount } from '@/components/common'
import { useNotification } from '@/lib/notifications'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { PlusIcon, ShareNetworkIcon } from '@phosphor-icons/react'
import { api, ApiError, refreshData, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import type {
  CatalogShare,
  CatalogShares,
  SharePassword,
  ConnectionFormat,
} from '@/lib/catalog-types'
import { dateTimeToISO, localDateTime, useDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { CopyButton } from '@/components/copy-button'
import { FormStatus } from '@/components/form-status'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { ConfirmAction, Empty, ErrorNotice, Loading } from '@/components/common'

function shareBody(share: CatalogShare) {
  return {
    format: share.format,
    media_types: share.media_types,
    stream_preferences: share.stream_preferences,
    name: share.name,
    enabled: share.enabled,
    scope: share.scope,
    source_ids: share.source_ids,
    fields: share.fields,
    revision: share.revision,
    expires_at: share.expires_at,
    requests_per_minute: share.requests_per_minute,
    max_concurrent_downloads: share.max_concurrent_downloads,
  }
}

function shareExpired(share: CatalogShare) {
  return Boolean(share.expires_at && Date.parse(share.expires_at) <= Date.now())
}

function connectionEndpoint(format: ConnectionFormat) {
  return format === 'stremio'
    ? '/stremio/connections'
    : format === 'torznab'
      ? '/torznab/endpoints'
      : '/shares'
}

export default function SharingPage({
  format = 'catalog',
  services = [],
}: {
  format?: ConnectionFormat
  services?: { id: string; name: string }[]
}) {
  const torznab = format === 'torznab'
  const stremio = format === 'stremio'
  const connection = format !== 'catalog'
  const endpoint = connectionEndpoint(format)
  const date = useDate()
  const returnFocus = useRef<HTMLElement | null>(null)
  const [editing, setEditing] = useState<CatalogShare | 'new' | null>(null)
  const [saving, setSaving] = useState(false)
  const [credential, setCredential] = useState<SharePassword | null>(null)
  const notify = useNotification()
  const shares = useQuery({
    queryKey: ['shares', format],
    queryFn: ({ signal }) => api<CatalogShares>(endpoint, { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
    structuralSharing: false,
  })
  const httpsReady = stremio || Boolean(shares.data?.public_url.startsWith('https://'))
  function rememberFocus() {
    returnFocus.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
  }
  function openEditor(share: CatalogShare | 'new') {
    rememberFocus()
    setEditing(share)
  }
  return (
    <div className="space-y-5">
      <div className="flex justify-end">
        <Button disabled={!shares.data} onClick={() => openEditor('new')}>
          <PlusIcon />
          {connection ? 'Create access' : 'Create share'}
        </Button>
      </div>
      {shares.data && !httpsReady && (
        <section className="notice notice-warning space-y-1">
          <h2 className="font-medium">HTTPS public address required</h2>
          <p>
            Set <code className="mono">PUBLIC_URL</code> to this instance’s HTTPS public origin and
            configure a trusted TLS certificate or HTTPS reverse proxy. Shares cannot be enabled
            without it. The browser’s address does not establish this configuration.
          </p>
        </section>
      )}
      <ErrorNotice error={shares.error} retry={() => void shares.refetch()} />
      {shares.data && (
        <ResultCount
          total={shares.data.items?.length ?? 0}
          label={connection ? 'access keys' : 'shares'}
        />
      )}
      {shares.isPending ? (
        <Loading />
      ) : shares.data?.items?.length ? (
        <div className="space-y-4">
          {shares.data.items.map((share) => (
            <section
              key={share.id}
              className="section-card"
              aria-label={`${connection ? 'Access' : 'Share'} ${share.name}`}
            >
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <h2 className="section-title break-words">{share.name}</h2>
                  <p className="mono mt-1 break-all text-muted-foreground">{share.id}</p>
                </div>
                <Badge
                  variant={
                    shareExpired(share) ? 'warning' : share.enabled ? 'success' : 'secondary'
                  }
                >
                  {shareExpired(share)
                    ? 'Expired'
                    : share.enabled
                      ? stremio
                        ? 'Enabled'
                        : 'Enabled · read-only'
                      : 'Disabled'}
                </Badge>
              </div>
              <dl className="grid gap-4 text-sm md:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">Source permissions</dt>
                  <dd className="mt-1 break-words">
                    {share.scope === 'all'
                      ? 'All current and future sources'
                      : share.source_ids
                          .map(
                            (id) =>
                              shares.data?.sources.find((source) => source.id === id)?.name || id,
                          )
                          .join(', ')}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {stremio ? 'Filters' : torznab ? 'Media permissions' : 'Authorized fields'}
                  </dt>
                  <dd className="mt-1 break-words">
                    {stremio
                      ? stremioPreferenceSummary(share.stream_preferences)
                      : torznab
                        ? share.media_types?.length
                          ? share.media_types
                              .map(
                                (type) =>
                                  ({ movie: 'Movies', tv: 'TV', other: 'Other' })[type] ?? type,
                              )
                              .join(', ')
                          : 'All media'
                        : share.fields.join(', ')}
                  </dd>
                </div>
                {!connection && (
                  <div>
                    <dt className="text-muted-foreground">Last completed download</dt>
                    <dd className="mt-1">
                      {share.last_sync_at ? date(share.last_sync_at) : 'No completed download'}
                    </dd>
                    <p className="help mt-1">
                      Final page delivered, not confirmation the recipient saved it.
                    </p>
                  </div>
                )}
                <div>
                  <dt className="text-muted-foreground">
                    {torznab ? 'Last search' : 'Last access'}
                  </dt>
                  <dd className="mt-1">
                    {share.last_access_at ? date(share.last_access_at) : 'No access recorded'}
                  </dd>
                </div>
                {(!connection || share.expires_at) && (
                  <div>
                    <dt className="text-muted-foreground">Expiration</dt>
                    <dd className="mt-1">
                      {share.expires_at ? date(share.expires_at) : 'No expiration'}
                    </dd>
                    {shareExpired(share) && (
                      <p className="help mt-1">
                        Access is blocked. Edit the expiration to restore access.
                      </p>
                    )}
                  </div>
                )}
                {(!connection ||
                  share.requests_per_minute > 0 ||
                  share.max_concurrent_downloads > 0) && (
                  <div>
                    <dt className="text-muted-foreground">
                      {connection ? 'Request limits' : 'Download limits'}
                    </dt>
                    <dd className="mt-1">
                      {share.requests_per_minute
                        ? `${share.requests_per_minute} requests per minute`
                        : 'No request limit'}{' '}
                      ·{' '}
                      {share.max_concurrent_downloads
                        ? `${share.max_concurrent_downloads} concurrent`
                        : 'No concurrency limit'}
                    </dd>
                    <p className="help mt-1">
                      Every request counts. Responses are limited to {stremio ? '70' : '20'}{' '}
                      seconds.
                    </p>
                  </div>
                )}
              </dl>
              {!connection && share.scope === 'all' && (
                <p className="help">
                  Future sources are automatically included. Use selected sources if new sources
                  should require your approval.
                </p>
              )}
              {!stremio && (
                <div className="field">
                  <Label htmlFor={`share-url-${share.id}`}>
                    {torznab
                      ? 'Torznab URL (no API key included)'
                      : 'Catalog URL (no password included)'}
                  </Label>
                  <Input
                    id={`share-url-${share.id}`}
                    readOnly
                    value={share.url}
                    placeholder="Available after HTTPS public origin is configured"
                    className="font-mono text-xs"
                  />
                  <CopyButton
                    value={share.url}
                    label={torznab ? 'Copy Torznab URL' : 'Copy catalog URL'}
                  />
                </div>
              )}
              {torznab && <TorznabSetup share={share} sources={shares.data?.sources ?? []} />}
              <div className="toolbar">
                <Button variant="outline" size="sm" onClick={() => openEditor(share)}>
                  Edit permissions & limits
                </Button>
                <ConfirmAction
                  title={`${share.enabled ? 'Disable' : 'Enable'} “${share.name}”?`}
                  description={
                    share.enabled
                      ? 'New requests will be denied immediately. Existing downloaded copies cannot be recalled. You can enable this share again later.'
                      : stremio
                        ? 'This installation grants Stremio access to the selected sources and playback services. Its saved filters apply immediately to every new request.'
                        : torznab
                          ? `This API key grants read-only access to published titles, verified identifiers and tracker-free magnets from ${share.scope === 'all' ? 'all current and future sources' : 'the selected sources'}.`
                          : `This password will grant read-only access to ${share.scope === 'all' ? 'all current and future sources' : 'the selected sources'}, limited to: ${share.fields.join(', ')}.`
                  }
                  label={share.enabled ? 'Disable access' : 'Enable access'}
                  variant={share.enabled ? 'warning' : 'default'}
                  trigger={
                    <Button
                      variant={share.enabled ? 'warning' : 'default'}
                      size="sm"
                      disabled={
                        !share.enabled &&
                        (!httpsReady || shareExpired(share) || share.fields.length === 0)
                      }
                    >
                      {share.enabled ? 'Disable' : 'Enable'}
                    </Button>
                  }
                  action={async () => {
                    await api<CatalogShare>(`${endpoint}/${encodeURIComponent(share.id)}`, {
                      method: 'PUT',
                      body: JSON.stringify({ ...shareBody(share), enabled: !share.enabled }),
                    })
                    notify(`Access ${share.enabled ? 'disabled' : 'enabled'} for “${share.name}”.`)
                    await refreshData()
                  }}
                />
                <ConfirmAction
                  title={`Replace ${connection ? 'access key' : 'password'} for “${share.name}”?`}
                  description={
                    stremio
                      ? 'The previous manifest and playback URLs will stop working. Reinstall using the replacement manifest URL. Settings and enabled state are unchanged.'
                      : 'The previous password will stop working immediately. Give the replacement to the recipient through a secure channel. Permissions and enabled state are unchanged.'
                  }
                  label={
                    stremio
                      ? 'Replace installation key'
                      : torznab
                        ? 'Rotate API key'
                        : 'Rotate password'
                  }
                  variant="warning"
                  trigger={
                    <Button variant="warning" size="sm">
                      {stremio
                        ? 'Replace installation key'
                        : torznab
                          ? 'Rotate API key'
                          : 'Rotate password'}
                    </Button>
                  }
                  action={async () => {
                    rememberFocus()
                    const result = await api<SharePassword>(
                      `${endpoint}/${encodeURIComponent(share.id)}/rotate`,
                      { method: 'POST', body: JSON.stringify({ revision: share.revision }) },
                    )
                    setCredential(result)
                    await refreshData()
                  }}
                />
                <ConfirmAction
                  title={`Revoke “${share.name}” permanently?`}
                  description={
                    stremio
                      ? 'This installation and all its playback URLs will stop working. Other installations are unaffected. Create a new access to install it again.'
                      : 'This share and its password will stop working. The recipient’s existing downloaded data is not deleted. Create a new share if you want to grant access again.'
                  }
                  label={connection ? 'Revoke access' : 'Revoke share'}
                  variant="destructive"
                  trigger={
                    <Button variant="destructive" size="sm">
                      Revoke
                    </Button>
                  }
                  action={async () => {
                    await api<void>(`${endpoint}/${encodeURIComponent(share.id)}`, {
                      method: 'DELETE',
                      body: JSON.stringify({ revision: share.revision }),
                    })
                    notify(`${connection ? 'Access' : 'Share'} “${share.name}” revoked.`)
                    await refreshData()
                  }}
                />
              </div>
            </section>
          ))}
        </div>
      ) : (
        !shares.isError && (
          <Empty
            icon={ShareNetworkIcon}
            title={
              stremio ? 'No Stremio access' : torznab ? 'No Torznab access' : 'No catalog shares'
            }
            description={
              stremio
                ? 'Create an access key, choose its sources and filters, then enable it and install the manifest in Stremio or AIOStreams.'
                : torznab
                  ? 'Create an access key, choose the allowed sources and media, then enable it for your client.'
                  : 'Create a disabled share, choose its source permissions and safe fields, then enable it when you are ready.'
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
          className="sm:max-w-2xl"
          showCloseButton={!saving}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            if (!credential) returnFocus.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {editing === 'new'
                ? connection
                  ? 'Create disabled access'
                  : 'Create disabled share'
                : 'Edit permissions & limits'}
            </DialogTitle>
            <DialogDescription>
              {stremio
                ? 'Create independent installations. Saved settings apply to the same link, and each access can be disabled, replaced or revoked separately.'
                : torznab
                  ? 'Published titles, verified identifiers and tracker-free magnets. No administrator announce URLs or debrid access.'
                  : 'Only the selected published fields are exposed. Passwords never grant access to administrative APIs.'}
            </DialogDescription>
          </DialogHeader>
          {editing && shares.data && (
            <ShareForm
              format={format}
              services={services}
              key={editing === 'new' ? 'new' : editing.id}
              share={editing === 'new' ? undefined : editing}
              data={shares.data}
              onSaving={setSaving}
              onSaved={(result) => {
                setEditing(null)
                if (result) setCredential(result)
                else notify(connection ? 'Access settings saved.' : 'Share permissions saved.')
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      {credential && (
        <PasswordDialog
          credential={credential}
          onClose={() => {
            setCredential(null)
            returnFocus.current?.focus()
          }}
        />
      )}
    </div>
  )
}

function ShareForm({
  format,
  services,
  share,
  data,
  onSaving,
  onSaved,
}: {
  format: ConnectionFormat
  services: { id: string; name: string }[]
  share?: CatalogShare
  data: CatalogShares
  onSaving: (saving: boolean) => void
  onSaved: (credential?: SharePassword) => void
}) {
  const torznab = format === 'torznab'
  const stremio = format === 'stremio'
  const connection = format !== 'catalog'
  const endpoint = connectionEndpoint(format)
  const [mediaTypes, setMediaTypes] = useState(share?.media_types ?? [])
  const [streamPreferences, setStreamPreferences] = useState(
    share?.stream_preferences ?? defaultStremioPreferences(),
  )
  const [original, setOriginal] = useState(share)
  const [name, setName] = useState(share?.name ?? '')
  const defaultScope = connection ? 'all' : 'selected'
  const requestsDefault = connection ? 0 : 120
  const concurrentDefault = connection ? 0 : 2
  const limitValue = (value: number | undefined, fallback: number) =>
    connection && (value ?? fallback) === 0 ? '' : String(value ?? fallback)
  const [scope, setScope] = useState<CatalogShare['scope']>(share?.scope ?? defaultScope)
  const [sourceIDs, setSourceIDs] = useState(share?.source_ids ?? [])
  const [fields, setFields] = useState(share?.fields ?? data.fields)
  const [expiresAt, setExpiresAt] = useState(localDateTime(share?.expires_at, 'seconds'))
  const [requestsPerMinute, setRequestsPerMinute] = useState(
    limitValue(share?.requests_per_minute, requestsDefault),
  )
  const [maxConcurrentDownloads, setMaxConcurrentDownloads] = useState(
    limitValue(share?.max_concurrent_downloads, concurrentDefault),
  )
  const [acknowledged, setAcknowledged] = useState(share?.scope === 'all')
  const [reloaded, setReloaded] = useState(false)
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (scope === 'selected' && sourceIDs.length === 0)
        throw new Error('Select at least one source.')
      if (!fields.length) throw new Error('Select at least one safe field.')
      const requests = Number(requestsPerMinute)
      const concurrent = Number(maxConcurrentDownloads)
      if (!Number.isInteger(requests) || requests < (connection ? 0 : 1) || requests > 3600)
        throw new Error(
          `Requests per minute must be a whole number between ${connection ? 0 : 1} and 3600.`,
        )
      if (!Number.isInteger(concurrent) || concurrent < (connection ? 0 : 1) || concurrent > 16)
        throw new Error(
          `Concurrent ${connection ? 'requests' : 'downloads'} must be a whole number between ${connection ? 0 : 1} and 16.`,
        )
      if (expiresAt && !Number.isFinite(new Date(expiresAt).getTime()))
        throw new Error('Choose a valid expiration date and time.')
      const body = {
        format,
        media_types: torznab ? mediaTypes : [],
        ...(stremio
          ? {
              stream_preferences: {
                ...streamPreferences,
                language: resolvedLanguage(streamPreferences),
              },
            }
          : {}),
        name: name.trim(),
        enabled: original?.enabled ?? false,
        scope,
        source_ids: scope === 'all' ? [] : sourceIDs,
        fields,
        expires_at: dateTimeToISO(expiresAt, original?.expires_at, 'seconds'),
        requests_per_minute: requests,
        max_concurrent_downloads: concurrent,
        ...(original ? { revision: original.revision } : {}),
      }
      if (original) {
        await api<CatalogShare>(`${endpoint}/${encodeURIComponent(original.id)}`, {
          method: 'PUT',
          body: JSON.stringify(body),
        })
        onSaved()
      } else {
        const result = await api<SharePassword>(endpoint, {
          method: 'POST',
          body: JSON.stringify(body),
        })
        onSaved(result)
      }
      await refreshData()
    },
    onMutate: () => onSaving(true),
    onSettled: () => onSaving(false),
  })
  const reload = useMutation({
    mutationFn: async () => {
      const latest = await api<CatalogShares>(endpoint)
      const current = latest.items?.find((item) => item.id === original?.id)
      if (!current)
        throw new Error(
          'This share was revoked. Close this draft and create a new share if needed.',
        )
      return current
    },
    onSuccess: (current) => {
      setOriginal(current)
      setReloaded(true)
      save.reset()
    },
  })
  const conflict = save.error instanceof ApiError && save.error.status === 409
  const busy = save.isPending || reload.isPending
  const dirty =
    (stremio &&
      JSON.stringify(streamPreferences) !==
        JSON.stringify(original?.stream_preferences ?? defaultStremioPreferences())) ||
    JSON.stringify(mediaTypes) !== JSON.stringify(original?.media_types ?? []) ||
    name !== (original?.name ?? '') ||
    scope !== (original?.scope ?? defaultScope) ||
    JSON.stringify(sourceIDs) !== JSON.stringify(original?.source_ids ?? []) ||
    JSON.stringify(fields) !== JSON.stringify(original?.fields ?? data.fields) ||
    expiresAt !== localDateTime(original?.expires_at, 'seconds') ||
    requestsPerMinute !== limitValue(original?.requests_per_minute, requestsDefault) ||
    maxConcurrentDownloads !== limitValue(original?.max_concurrent_downloads, concurrentDefault) ||
    (!connection && !original && acknowledged)
  useUnsavedChanges(
    dirty,
    stremio ? 'Stremio access' : torznab ? 'Torznab access' : 'catalog sharing',
    busy,
  )
  const unavailable = sourceIDs.filter((id) => !data.sources.some((source) => source.id === id))
  const sourceOptions = [
    ...data.sources,
    ...unavailable.map((id) => ({ id, name: `${id} (unavailable)` })),
  ]
  const selectSources = (ids: string[]) => {
    setSourceIDs(ids)
    setScope(ids.length ? 'selected' : 'all')
  }
  const sourceFilter = (
    <SourceFilter
      id="share-sources"
      value={sourceIDs}
      sources={sourceOptions}
      onChange={selectSources}
    />
  )
  const accessPolicy = (
    <fieldset className={connection ? 'space-y-4' : 'space-y-4 border-t border-border pt-4'}>
      {!connection && <legend className="px-1 text-sm font-medium">Access policy</legend>}
      <div className="field">
        <Label htmlFor="share-expires">Expires at (browser local time)</Label>
        <Input
          id="share-expires"
          type="datetime-local"
          step="1"
          value={expiresAt}
          onChange={(event) => setExpiresAt(event.target.value)}
          aria-describedby="share-expires-help"
        />
        <p id="share-expires-help" className="help">
          Uses the browser's timezone. Leave empty for no expiration. Expired access cannot be used.
        </p>
        {expiresAt && new Date(expiresAt).getTime() <= Date.now() && (
          <p className="help text-destructive">
            This date is in the past. Access will remain blocked.
          </p>
        )}
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="field">
          <Label htmlFor="share-request-limit">Requests per minute</Label>
          <Input
            id="share-request-limit"
            type="number"
            required={!connection}
            placeholder={connection ? 'No limit' : undefined}
            min={connection ? 0 : 1}
            max="3600"
            step="1"
            value={requestsPerMinute}
            onChange={(event) => setRequestsPerMinute(event.target.value)}
            aria-describedby="share-limits-help"
          />
        </div>
        <div className="field">
          <Label htmlFor="share-concurrent-limit">
            {connection ? 'Concurrent requests' : 'Concurrent downloads'}
          </Label>
          <Input
            id="share-concurrent-limit"
            type="number"
            required={!connection}
            placeholder={connection ? 'No limit' : undefined}
            min={connection ? 0 : 1}
            max="16"
            step="1"
            value={maxConcurrentDownloads}
            onChange={(event) => setMaxConcurrentDownloads(event.target.value)}
            aria-describedby="share-limits-help"
          />
        </div>
      </div>
      <p id="share-limits-help" className="help">
        {connection
          ? 'Leave empty or enter 0 for no limit. Limits apply to all clients using this access key.'
          : 'Limits apply across all clients using this access key and all server instances. Every authenticated request counts toward a rolling minute. Clients exceeding a limit receive HTTP 429 with a retry delay.'}
      </p>
    </fieldset>
  )
  return (
    <form
      className="space-y-5"
      onSubmit={(event) => {
        event.preventDefault()
        if (!busy && !conflict && (!original || dirty)) save.mutate()
      }}
    >
      <fieldset className="space-y-5" disabled={busy}>
        <div className="field">
          <Label htmlFor="share-name">
            {connection ? 'Access name' : 'Recipient / share name'}
          </Label>
          <Input
            id="share-name"
            required
            maxLength={120}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        {!connection && (
          <>
            <div className="field">
              <Label htmlFor="share-scope">Source access</Label>
              <select
                id="share-scope"
                className="native-select w-full"
                value={scope}
                onChange={(event) => {
                  setScope(event.target.value as CatalogShare['scope'])
                  setAcknowledged(false)
                }}
              >
                <option value="selected">Selected sources only</option>
                <option value="all">All current and future sources</option>
              </select>
            </div>
            {scope === 'selected' ? (
              <fieldset className="space-y-3">
                <legend className="mb-2 text-sm font-medium">Allowed sources</legend>
                {data.sources.length === 0 && (
                  <p className="help">
                    No sources are available. Add a source before creating a share for selected
                    sources.
                  </p>
                )}
                <div className="grid max-h-56 gap-3 overflow-y-auto p-1 sm:grid-cols-2">
                  {[
                    ...data.sources,
                    ...unavailable.map((id) => ({
                      id,
                      name: `${id} (unavailable; remove before saving)`,
                    })),
                  ].map((source) => (
                    <label key={source.id} className="flex items-start gap-2 text-sm">
                      <input
                        type="checkbox"
                        className="mt-1 accent-primary"
                        checked={sourceIDs.includes(source.id)}
                        onChange={(event) =>
                          setSourceIDs(
                            event.target.checked
                              ? [...sourceIDs, source.id]
                              : sourceIDs.filter((id) => id !== source.id),
                          )
                        }
                      />
                      <span className="break-words">{source.name || source.id}</span>
                    </label>
                  ))}
                </div>
              </fieldset>
            ) : (
              <div className="notice space-y-3">
                <p>
                  Access to all sources automatically includes sources added in the future,
                  including imported catalogs that are published locally. New sources will not
                  require another approval.
                </p>
                <label className="flex items-start gap-2">
                  <input
                    type="checkbox"
                    required
                    checked={acknowledged}
                    onChange={(event) => setAcknowledged(event.target.checked)}
                    className="mt-1 accent-primary"
                  />
                  <span>I authorize access to all current and future sources.</span>
                </label>
              </div>
            )}
          </>
        )}
        {stremio ? (
          <StremioConnectionFilters
            value={streamPreferences}
            onChange={setStreamPreferences}
            services={services}
            sources={sourceOptions}
            sourceIDs={sourceIDs}
            onSourceIDsChange={selectSources}
          />
        ) : torznab ? (
          <Disclosure title="Filters">
            {sourceFilter}
            <div className="field">
              <Label htmlFor="torznab-media">Media type</Label>
              <select
                id="torznab-media"
                className="native-select w-full"
                value={mediaTypes.length ? mediaTypes[0] : ''}
                onChange={(event) => setMediaTypes(event.target.value ? [event.target.value] : [])}
              >
                <option value="">All media</option>
                <option value="movie">Movies</option>
                <option value="tv">TV</option>
                <option value="other">Other / not identified</option>
              </select>
            </div>
          </Disclosure>
        ) : (
          <>
            {' '}
            <fieldset className="space-y-3">
              <legend className="mb-2 text-sm font-medium">Safe fields to share</legend>
              <div className="grid gap-3 sm:grid-cols-2">
                {data.fields.map((field) => (
                  <label key={field} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      className="accent-primary"
                      checked={fields.includes(field)}
                      onChange={(event) =>
                        setFields(
                          event.target.checked
                            ? [...fields, field]
                            : fields.filter((value) => value !== field),
                        )
                      }
                    />
                    <span className="mono">{field}</span>
                  </label>
                ))}
              </div>
              <p className="help">
                No download URLs, magnets, tracker passkeys, arbitrary metadata or raw data.
                Original provenance accompanies authorized fields.
              </p>
            </fieldset>
          </>
        )}
        {connection ? <Disclosure title="Advanced">{accessPolicy}</Disclosure> : accessPolicy}
      </fieldset>
      <p className="help">
        {original
          ? `Access remains ${original.enabled ? 'enabled' : 'disabled'}, unless expired. Settings apply to every new request${stremio ? ' without reinstalling' : ', including saved cursors'}.`
          : `Access starts disabled. Save the generated ${stremio ? 'manifest URL' : connection ? 'API key' : 'password'} securely; enable access separately when ready.`}
      </p>
      <ErrorNotice error={save.error} />
      <ErrorNotice error={reload.error} />
      {conflict && (
        <div className="notice space-y-3">
          <p>
            The share changed on the server. Your permission draft is preserved. Reload its current
            revision, then review before replacing its permissions. Its current enabled state will
            be kept.
          </p>
          <Button type="button" variant="outline" disabled={busy} onClick={() => reload.mutate()}>
            Reload revision, keep draft
          </Button>
        </div>
      )}
      {reloaded && !conflict && (
        <p className="notice" role="status">
          Current revision loaded. Review your permission draft before saving.
        </p>
      )}
      <FormStatus dirty={dirty} saving={save.isPending} />
      <DialogFooter>
        <Button
          type="submit"
          disabled={
            busy ||
            Boolean(original && !dirty) ||
            conflict ||
            !name.trim() ||
            fields.length === 0 ||
            (scope === 'selected' ? sourceIDs.length === 0 : !connection && !acknowledged)
          }
        >
          {save.isPending
            ? 'Saving…'
            : original
              ? 'Save permissions'
              : connection
                ? 'Create disabled access'
                : 'Create disabled share'}
        </Button>
      </DialogFooter>
    </form>
  )
}

function PasswordDialog({
  credential,
  onClose,
}: {
  credential: SharePassword
  onClose: () => void
}) {
  const [visible, setVisible] = useState(false)
  const stremio = credential.share.format === 'stremio'
  const torznab = credential.share.format === 'torznab'
  const kind = stremio ? 'manifest URL' : torznab ? 'API key' : 'password'
  const manifest = stremio
    ? new URL(
        `${credential.share.url}.${credential.password}/manifest.json`,
        window.location.origin,
      ).toString()
    : ''
  const secret = stremio ? manifest : credential.password
  return (
    <Dialog
      open
      unsavedChanges={{ dirty: true, label: `${credential.share.format || 'catalog'} ${kind}` }}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Save this {kind} now</DialogTitle>
          <DialogDescription>
            This is the only time the {kind} for “{credential.share.name}” is shown. Closing this
            dialog clears it from the page. The server cannot retrieve it later.
          </DialogDescription>
        </DialogHeader>
        <div className="field">
          <Label htmlFor="created-share-password">
            {stremio ? 'Manifest URL' : torznab ? 'Torznab API key' : 'Sharing password'}
          </Label>
          <Input
            id="created-share-password"
            type={visible ? 'text' : 'password'}
            readOnly
            autoComplete="off"
            spellCheck={false}
            value={secret}
            className="font-mono text-xs"
          />
        </div>
        <div className="toolbar items-start">
          <Button variant="outline" onClick={() => setVisible(!visible)}>
            {visible ? 'Hide' : 'Reveal'} {kind}
          </Button>
          <CopyButton
            value={secret}
            label={`Copy ${kind}`}
            variant={stremio ? 'default' : 'outline'}
            manualCopyMessage={`Clipboard access is unavailable. Reveal the ${kind} and copy it manually.`}
            successMessage="Copied to your clipboard. Keep it private."
          />
          {stremio && (
            <Button asChild variant="outline">
              <a href={`stremio://${manifest.replace(/^https?:\/\//, '')}`}>Install in Stremio</a>
            </Button>
          )}
        </div>
        <p className="notice">
          {shareExpired(credential.share)
            ? 'This access has expired; update its expiration before using it.'
            : credential.share.enabled
              ? 'This access is enabled.'
              : 'This access is disabled; enable it separately.'}{' '}
          {stremio
            ? 'Use the manifest in Stremio or AIOStreams. Keep the link private. Saved settings apply without reinstalling.'
            : torznab
              ? 'Enter the URL and API key in your Torznab client. Keep the key private; it grants access to the permitted catalog.'
              : 'Send the catalog URL and password through a secure channel. Never append the password to the URL.'}
        </p>
        {!stremio && credential.share.url && (
          <div className="field">
            <Label htmlFor="created-share-url">{torznab ? 'Torznab URL' : 'Catalog URL'}</Label>
            <Input
              id="created-share-url"
              readOnly
              value={credential.share.url}
              className="font-mono text-xs"
            />
            <CopyButton
              value={credential.share.url}
              label={torznab ? 'Copy Torznab URL' : 'Copy catalog URL'}
            />
          </div>
        )}
        <DialogFooter>
          <Button onClick={onClose}>I have saved the {kind}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
