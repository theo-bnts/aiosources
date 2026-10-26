import { catalogEntitiesKey, type CatalogEntities } from '@/lib/catalog-types'
import { MultiSelect } from '@/components/ui/multi-select'
import { CatalogDataControls, catalogEntitiesSummary } from '@/components/catalog-data-controls'
import { ResultSorting } from '@/components/result-controls'
import { useSettingsEditor } from '@/lib/settings-navigation'
import { DateTimeInput, useDateTimeZoneLabel } from '@/components/date-time-input'
import { SourceFilter } from '@/components/source-filter'
import { Statistic } from '@/components/statistic'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'
import { Switch } from '@/components/ui/switch'
import { Disclosure } from '@/components/ui/disclosure'
import { TorznabFilters } from '@/components/torznab-filters'
import {
  StremioConnectionFilters,
  defaultStremioPreferences,
  stremioPreferencesKey,
} from '@/components/stremio-connection-filters'
import { resolvedLanguage } from '@/lib/language-preferences'
import { torznabCachePreferences } from '@/lib/torznab-preferences'
import { ResultCount, ConfirmAction, Empty, ErrorNotice, Loading } from '@/components/common'
import { ListFilters, useListFilters } from '@/components/list-filters'
import { compareListNames, matchesListQuery } from '@/lib/list-view'
import {
  catalogFieldLabel,
  catalogFieldsSummary,
  catalogShareState,
  connectionSummary,
} from '@/lib/catalog-display'
import { useNotification } from '@/lib/notifications'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import {
  PlusIcon,
  ShareNetworkIcon,
  PencilSimpleIcon,
  TrashIcon,
  ArrowClockwiseIcon,
} from '@phosphor-icons/react'
import { api, ApiError, refreshData, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import type {
  CatalogShare,
  CatalogShares,
  SharePassword,
  ConnectionFormat,
  TorznabPreferences,
} from '@/lib/catalog-types'
import { useDate } from '@/lib/format'
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

function shareBody(share: CatalogShare) {
  return {
    format: share.format,
    catalog_entities: share.catalog_entities,
    media_types: share.media_types,
    stream_preferences: share.stream_preferences,
    torznab_preferences: share.torznab_preferences,
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
  return catalogShareState(share) === 'expired'
}

const accessStates = [
  { value: 'enabled', label: 'Enabled' },
  { value: 'disabled', label: 'Disabled' },
  { value: 'expired', label: 'Expired' },
]

function matchesShareFilters(share: CatalogShare, filters: ReturnType<typeof useListFilters>) {
  return (
    matchesListQuery(filters.query, share.name, share.id) &&
    (!filters.state || filters.state === catalogShareState(share))
  )
}

function connectionEndpoint(format: ConnectionFormat) {
  return format === 'stremio'
    ? '/stremio/connections'
    : format === 'torznab'
      ? '/torznab/endpoints'
      : '/shares'
}

function useShares(format: ConnectionFormat) {
  const endpoint = connectionEndpoint(format)
  return useQuery({
    queryKey: ['shares', format],
    queryFn: ({ signal }) => api<CatalogShares>(endpoint, { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
    structuralSharing: false,
  })
}

export function SharedCatalogStatisticsPage() {
  const filters = useListFilters()
  const query = useShares('catalog')
  const date = useDate()
  const items = [...(query.data?.items ?? [])].sort(
    (a, b) =>
      (b.last_access_at ?? '').localeCompare(a.last_access_at ?? '') ||
      a.name.localeCompare(b.name),
  )
  const visible = items.filter((share) => matchesShareFilters(share, filters))
  const filtered = Boolean(filters.query || filters.state)
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading />
      ) : (
        query.data && (
          <>
            <dl className="grid grid-cols-2 gap-3 xl:grid-cols-4">
              <Statistic label="Shares" value={items.length} />
              <Statistic
                label="Active shares"
                value={items.filter((share) => share.enabled && !shareExpired(share)).length}
              />
              <Statistic
                label="Shares downloaded"
                value={items.filter((share) => share.last_sync_at).length}
              />
              <Statistic label="Expired shares" value={items.filter(shareExpired).length} />
            </dl>
            {items.length || filtered ? (
              <ListFilters
                kind="share"
                filters={filters}
                states={accessStates}
                count={visible.length}
                total={items.length}
              />
            ) : (
              <ResultCount total={0} label="shares" />
            )}
            {visible.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Recipient</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Last access</TableHead>
                      <TableHead>Last completed download</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visible.map((share) => (
                      <TableRow key={share.id}>
                        <TableCell className="font-medium whitespace-normal">
                          {share.name}
                        </TableCell>
                        <TableCell>
                          <Badge
                            variant={
                              shareExpired(share)
                                ? 'warning'
                                : share.enabled
                                  ? 'success'
                                  : 'secondary'
                            }
                          >
                            {shareExpired(share)
                              ? 'Expired'
                              : share.enabled
                                ? 'Enabled'
                                : 'Disabled'}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          {share.last_access_at ? date(share.last_access_at) : 'Never accessed'}
                        </TableCell>
                        <TableCell>
                          {share.last_sync_at ? date(share.last_sync_at) : 'Never completed'}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <Empty
                title={filtered ? 'No shares match these filters' : 'No shared catalogs'}
                description={
                  filtered
                    ? 'Clear the filters to see your other shares.'
                    : 'Create a share in Settings to grant access to your published torrents.'
                }
              />
            )}
            {items.length > 0 && (
              <p className="help">
                A completed download means the final page was delivered; the recipient confirms its
                own import separately.
              </p>
            )}
          </>
        )
      )}
    </div>
  )
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
  const shares = useShares(format)
  const filters = useListFilters()
  const items = [...(shares.data?.items ?? [])].sort(compareListNames)
  const visible = items.filter((share) => matchesShareFilters(share, filters))
  const filtered = Boolean(filters.query || filters.state)
  useSettingsEditor(
    format === 'catalog' ? 'share' : format,
    shares.data ? (shares.data.items ?? []) : undefined,
    (share) => openEditor(share),
    () => setEditing(null),
    editing === 'new' ? 'new' : (editing?.id ?? null),
  )
  const originReady =
    stremio ||
    Boolean(
      shares.data?.public_url.startsWith('https://') ||
      (torznab && shares.data?.public_url.startsWith('http://')),
    )
  function rememberFocus() {
    returnFocus.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
  }
  function openEditor(share: CatalogShare | 'new') {
    rememberFocus()
    setEditing(share)
  }
  return (
    <div className={format === 'catalog' ? 'page' : 'space-y-5'}>
      <div className="flex justify-end">
        <Button disabled={!shares.data} onClick={() => openEditor('new')}>
          <PlusIcon />
          {connection ? 'Create access' : 'Create share'}
        </Button>
      </div>
      {shares.data && !originReady && (
        <section className="notice notice-warning space-y-1">
          <h2 className="font-medium">
            {torznab ? 'Connection address required' : 'HTTPS public address required'}
          </h2>
          <p>
            {torznab ? (
              <>
                Set <code className="mono">PUBLIC_URL</code> to this instance’s HTTP or HTTPS origin
                used by your clients. Access keys cannot be enabled until it is configured.
              </>
            ) : (
              <>
                Set <code className="mono">PUBLIC_URL</code> to this instance’s HTTPS public origin
                and configure a trusted TLS certificate or HTTPS reverse proxy. Shares cannot be
                enabled without it. The browser’s address does not establish this configuration.
              </>
            )}
          </p>
        </section>
      )}
      <ErrorNotice error={shares.error} retry={() => void shares.refetch()} />
      {shares.data &&
        (items.length || filtered ? (
          <ListFilters
            kind={connection ? 'access' : 'share'}
            filters={filters}
            states={accessStates}
            count={visible.length}
            total={items.length}
          />
        ) : (
          <ResultCount total={0} label={connection ? 'access keys' : 'shares'} />
        ))}
      {shares.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="space-y-4">
          {visible.map((share) => {
            const summary = connectionSummary(share, format)
            return (
              <section
                key={share.id}
                className="section-card"
                aria-label={`${connection ? 'Access' : 'Share'} ${share.name}`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <h2 className="section-title break-words">{share.name}</h2>
                    <p className="mono mt-1 break-all text-muted-foreground">{share.id}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-3">
                    {shareExpired(share) && <Badge variant="warning">Expired</Badge>}
                    <ConfirmAction
                      title={`${share.enabled ? 'Disable' : 'Enable'} “${share.name}”?`}
                      description={
                        share.enabled
                          ? 'New requests will be denied immediately. Existing downloaded copies cannot be recalled. You can enable this access again later.'
                          : stremio
                            ? 'This installation grants Stremio access to the selected sources and playback services. Its saved filters apply immediately to every new request.'
                            : torznab
                              ? `This API key grants read-only access to published titles, verified identifiers and tracker-free magnets from ${share.scope === 'all' ? 'all current and future sources' : 'the selected sources'}.`
                              : `This password will grant read-only access to ${share.scope === 'all' ? 'all current and future sources' : 'the selected sources'}, limited to: ${catalogFieldsSummary(share.fields)}.`
                      }
                      label={share.enabled ? 'Disable access' : 'Enable access'}
                      variant={share.enabled ? 'warning' : 'default'}
                      trigger={
                        <Switch
                          checked={share.enabled}
                          aria-label={`${share.enabled ? 'Disable' : 'Enable'} ${share.name}`}
                          disabled={
                            !share.enabled &&
                            (!originReady || shareExpired(share) || share.fields.length === 0)
                          }
                        />
                      }
                      action={async () => {
                        await api<CatalogShare>(`${endpoint}/${encodeURIComponent(share.id)}`, {
                          method: 'PUT',
                          body: JSON.stringify({ ...shareBody(share), enabled: !share.enabled }),
                        })
                        notify(
                          `Access ${share.enabled ? 'disabled' : 'enabled'} for “${share.name}”.`,
                        )
                        await refreshData()
                      }}
                    />
                  </div>
                </div>
                <dl className="grid gap-4 text-sm md:grid-cols-2">
                  <div>
                    <dt className="text-muted-foreground">Sources</dt>
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
                      {connection ? 'Filters' : 'Published fields'}
                    </dt>
                    <dd className="mt-1 break-words">
                      {connection ? (
                        summary?.filters || 'All results'
                      ) : (
                        <>
                          {catalogFieldsSummary(share.fields)}
                          {Object.values(share.catalog_entities ?? {}).some(Boolean) && (
                            <span className="mt-1 block text-muted-foreground">
                              {catalogEntitiesSummary(share.catalog_entities)}
                            </span>
                          )}
                        </>
                      )}
                    </dd>
                  </div>
                  {summary?.sorting && (
                    <div>
                      <dt className="text-muted-foreground">Sorting</dt>
                      <dd className="mt-1 break-words">{summary.sorting}</dd>
                    </div>
                  )}
                  {summary?.limits && (
                    <div>
                      <dt className="text-muted-foreground">Result limits</dt>
                      <dd className="mt-1 break-words">{summary.limits}</dd>
                    </div>
                  )}
                  {summary?.response && (
                    <div>
                      <dt className="text-muted-foreground">
                        {stremio ? 'Stream response timeout' : 'Response timeout'}
                      </dt>
                      <dd className="mt-1">{summary.response}</dd>
                    </div>
                  )}
                  {connection && (
                    <div>
                      <dt className="text-muted-foreground">
                        {torznab ? 'Last search' : 'Last access'}
                      </dt>
                      <dd className="mt-1">
                        {share.last_access_at ? date(share.last_access_at) : 'No access recorded'}
                      </dd>
                    </div>
                  )}
                  {share.expires_at && (
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
                      <dt className="text-muted-foreground">Request limits</dt>
                      <dd className="mt-1">
                        {share.requests_per_minute
                          ? `${share.requests_per_minute} requests per minute`
                          : 'No request limit'}{' '}
                        ·{' '}
                        {share.max_concurrent_downloads
                          ? `${share.max_concurrent_downloads} concurrent`
                          : 'No concurrency limit'}
                      </dd>
                    </div>
                  )}
                </dl>
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
                      placeholder={
                        torznab
                          ? 'Available after the connection address is configured'
                          : 'Available after HTTPS public origin is configured'
                      }
                      className="font-mono text-xs"
                    />
                    <CopyButton
                      value={share.url}
                      label={torznab ? 'Copy Torznab URL' : 'Copy catalog URL'}
                    />
                  </div>
                )}
                <div className="toolbar">
                  <Button variant="outline" size="sm" onClick={() => openEditor(share)}>
                    <PencilSimpleIcon />
                    Edit
                  </Button>
                  <ConfirmAction
                    title={`Replace ${stremio ? 'installation key' : torznab ? 'API key' : 'password'} for “${share.name}”?`}
                    description={
                      stremio
                        ? 'The previous manifest and playback URLs will stop working. Reinstall using the replacement manifest URL. Settings and enabled state are unchanged.'
                        : `The previous ${torznab ? 'API key' : 'password'} will stop working immediately. Give the replacement to the recipient through a secure channel. Permissions and enabled state are unchanged.`
                    }
                    label={
                      stremio
                        ? 'Replace installation key'
                        : torznab
                          ? 'Replace API key'
                          : 'Replace password'
                    }
                    variant="warning"
                    trigger={
                      <Button variant="warning" size="sm">
                        <ArrowClockwiseIcon />
                        {stremio
                          ? 'Replace installation key'
                          : torznab
                            ? 'Replace API key'
                            : 'Replace password'}
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
                    title={`Delete “${share.name}” permanently?`}
                    description={
                      stremio
                        ? 'This installation and all its playback URLs will stop working. Other installations are unaffected. Create a new access to install it again.'
                        : torznab
                          ? 'This access and its API key will stop working. Other access keys are unaffected. Create a new access to connect again.'
                          : 'This share and its password will stop working. The recipient’s existing downloaded data is not deleted. Create a new share if you want to grant access again.'
                    }
                    label={connection ? 'Delete access' : 'Delete share'}
                    variant="destructive"
                    trigger={
                      <Button variant="destructive" size="sm">
                        <TrashIcon />
                        Delete
                      </Button>
                    }
                    action={async () => {
                      await api<void>(`${endpoint}/${encodeURIComponent(share.id)}`, {
                        method: 'DELETE',
                        body: JSON.stringify({ revision: share.revision }),
                      })
                      notify(`${connection ? 'Access' : 'Share'} “${share.name}” deleted.`)
                      await refreshData()
                    }}
                  />
                </div>
              </section>
            )
          })}
        </div>
      ) : (
        !shares.isError && (
          <Empty
            icon={ShareNetworkIcon}
            title={
              filtered
                ? connection
                  ? 'No access keys match these filters'
                  : 'No shares match these filters'
                : stremio
                  ? 'No Stremio access'
                  : torznab
                    ? 'No Torznab access'
                    : 'No catalog shares'
            }
            description={
              filtered
                ? `Clear the filters to see your other ${connection ? 'access keys' : 'shares'}.`
                : stremio
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
          data-settings-editor={format === 'catalog' ? 'share' : format}
          data-settings-entity={editing === 'new' ? 'new' : editing?.id}
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
                  ? 'Create access'
                  : 'Create share'
                : connection
                  ? 'Edit access'
                  : 'Edit share'}
            </DialogTitle>
            <DialogDescription>
              {stremio
                ? 'Create independent installations. Saved settings apply to the same link, and each access can be disabled, replaced or revoked separately.'
                : torznab
                  ? 'Published titles, verified identifiers and tracker-free magnets. No administrator announce URLs or debrid access.'
                  : 'Choose the sources and fields shared with this recipient.'}
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
                else notify(connection ? 'Access settings saved.' : 'Share settings saved.')
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
  const [torznabPreferences, setTorznabPreferences] = useState<TorznabPreferences>(
    torznabCachePreferences(share?.torznab_preferences ?? {}),
  )
  const [original, setOriginal] = useState(share)
  const [name, setName] = useState(share?.name ?? '')
  const defaultScope = 'all'
  const requestsDefault = connection ? 0 : 120
  const concurrentDefault = connection ? 0 : 2
  const limitValue = (value: number | undefined, fallback: number) =>
    connection && (value ?? fallback) === 0 ? '' : String(value ?? fallback)
  const [scope, setScope] = useState<CatalogShare['scope']>(share?.scope ?? defaultScope)
  const [sourceIDs, setSourceIDs] = useState(share?.source_ids ?? [])
  const [fields, setFields] = useState(share?.fields ?? data.fields)
  const [catalogEntities, setCatalogEntities] = useState<CatalogEntities>(
    share?.catalog_entities ?? {},
  )
  const dateZone = useDateTimeZoneLabel()
  const [expiresAt, setExpiresAt] = useState(share?.expires_at ?? '')
  const [requestsPerMinute, setRequestsPerMinute] = useState(
    limitValue(share?.requests_per_minute, requestsDefault),
  )
  const [maxConcurrentDownloads, setMaxConcurrentDownloads] = useState(
    limitValue(share?.max_concurrent_downloads, concurrentDefault),
  )
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
        ...(!connection ? { catalog_entities: catalogEntities } : {}),
        media_types: torznab ? mediaTypes : [],
        ...(torznab ? { torznab_preferences: torznabPreferences } : {}),
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
        expires_at: expiresAt || null,
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
    (torznab &&
      JSON.stringify(torznabPreferences) !==
        JSON.stringify(torznabCachePreferences(original?.torznab_preferences ?? {}))) ||
    (stremio &&
      stremioPreferencesKey(streamPreferences) !==
        stremioPreferencesKey(original?.stream_preferences ?? defaultStremioPreferences())) ||
    JSON.stringify(mediaTypes) !== JSON.stringify(original?.media_types ?? []) ||
    name !== (original?.name ?? '') ||
    scope !== (original?.scope ?? defaultScope) ||
    JSON.stringify(sourceIDs) !== JSON.stringify(original?.source_ids ?? []) ||
    (!connection &&
      catalogEntitiesKey(catalogEntities) !== catalogEntitiesKey(original?.catalog_entities)) ||
    [...fields].sort().join(',') !== [...(original?.fields ?? data.fields)].sort().join(',') ||
    expiresAt !== (original?.expires_at ?? '') ||
    requestsPerMinute !== limitValue(original?.requests_per_minute, requestsDefault) ||
    maxConcurrentDownloads !== limitValue(original?.max_concurrent_downloads, concurrentDefault)
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
  const expiration = (
    <div className="field">
      <Label htmlFor="share-expires">Expires at ({dateZone})</Label>
      <DateTimeInput
        id="share-expires"
        precision="seconds"
        value={expiresAt}
        onValueChange={setExpiresAt}
        aria-describedby="share-expires-help"
      />
      <p id="share-expires-help" className="help">
        Uses {dateZone}. Leave empty for no expiration. Expired access cannot be used.
      </p>
      {expiresAt && new Date(expiresAt).getTime() <= Date.now() && (
        <p className="help text-destructive">
          This date is in the past. Access will remain blocked.
        </p>
      )}
    </div>
  )
  const requestLimits = (
    <fieldset className="min-w-0 space-y-4">
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
      <fieldset className="min-w-0 space-y-5" disabled={busy}>
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
          <>
            <Disclosure title="Filters">
              <TorznabFilters
                value={{ ...torznabPreferences, media: mediaTypes, sources: sourceIDs }}
                onChange={({ media, sources, ...options }) => {
                  setMediaTypes(media ?? [])
                  selectSources(sources ?? [])
                  setTorznabPreferences(torznabCachePreferences(options))
                }}
                sources={sourceOptions}
                showSources
              />
              <p className="help">
                Services select the local availability cache. Configure downloads in your client.
              </p>
            </Disclosure>
            <ResultSorting
              value={torznabPreferences.sorting}
              onChange={(sorting) => setTorznabPreferences({ ...torznabPreferences, sorting })}
              defaultOrder="newest first"
            />
          </>
        ) : (
          <Disclosure title="Filters">
            <SourceFilter
              id="share-sources"
              value={sourceIDs}
              sources={sourceOptions}
              onChange={selectSources}
            />
          </Disclosure>
        )}
        {!connection && (
          <CatalogDataControls
            id="share-data"
            value={catalogEntities}
            onChange={setCatalogEntities}
            allowAvailability={fields.includes('info_hash')}
            publishedFields={
              <div className="field">
                <Label htmlFor="share-fields">Published fields</Label>
                <MultiSelect
                  id="share-fields"
                  label="Published fields"
                  allLabel="All published fields"
                  value={fields}
                  options={data.fields.map((field) => ({
                    value: field,
                    label: catalogFieldLabel(field),
                  }))}
                  onChange={(selection) => {
                    const next = selection.length
                      ? data.fields.filter((field) => selection.includes(field))
                      : data.fields
                    setFields(next)
                    if (!next.includes('info_hash'))
                      setCatalogEntities((current) => ({ ...current, availability: false }))
                  }}
                />
              </div>
            }
          />
        )}
        <Disclosure title="Advanced">
          <Disclosure title="Request limits">{requestLimits}</Disclosure>
          <Disclosure title="Expiration">{expiration}</Disclosure>
        </Disclosure>
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
            This {connection ? 'access' : 'share'} changed on the server. Your settings draft is
            preserved. Reload its current revision, then review before saving. Its current enabled
            state will be kept.
          </p>
          <Button type="button" variant="outline" disabled={busy} onClick={() => reload.mutate()}>
            Reload revision, keep draft
          </Button>
        </div>
      )}
      {reloaded && !conflict && (
        <p className="notice" role="status">
          Current revision loaded. Review your settings draft before saving.
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
            (scope === 'selected' && sourceIDs.length === 0)
          }
        >
          {save.isPending
            ? 'Saving…'
            : original
              ? 'Save'
              : connection
                ? 'Create access'
                : 'Create share'}
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
            type="password"
            revealLabel={kind}
            readOnly
            autoComplete="off"
            spellCheck={false}
            value={secret}
            className="font-mono text-xs"
          />
        </div>
        <div className="toolbar items-start">
          <CopyButton
            value={secret}
            label={`Copy ${kind}`}
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
