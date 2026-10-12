import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { PlusIcon, ShareNetworkIcon } from '@phosphor-icons/react'
import { api, ApiError, refreshData, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import type { CatalogShare, CatalogShares, SharePassword } from '@/lib/catalog-types'
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

export default function SharingPage() {
  const date = useDate()
  const returnFocus = useRef<HTMLElement | null>(null)
  const [editing, setEditing] = useState<CatalogShare | 'new' | null>(null)
  const [saving, setSaving] = useState(false)
  const [credential, setCredential] = useState<SharePassword | null>(null)
  const [notice, setNotice] = useState('')
  const shares = useQuery({
    queryKey: ['shares'],
    queryFn: ({ signal }) => api<CatalogShares>('/shares', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
    structuralSharing: false,
  })
  const httpsReady = Boolean(shares.data?.public_url.startsWith('https://'))
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
          Create share
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
      {notice && (
        <p className="notice text-primary" role="status">
          {notice}
        </p>
      )}
      <ErrorNotice error={shares.error} retry={() => void shares.refetch()} />
      {shares.isPending ? (
        <Loading />
      ) : shares.data?.items?.length ? (
        <div className="space-y-4">
          {shares.data.items.map((share) => (
            <section key={share.id} className="section-card" aria-label={`Share ${share.name}`}>
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
                      ? 'Enabled · read-only'
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
                  <dt className="text-muted-foreground">Authorized fields</dt>
                  <dd className="mono mt-1 break-words">{share.fields.join(', ')}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Last completed download</dt>
                  <dd className="mt-1">
                    {share.last_sync_at ? date(share.last_sync_at) : 'No completed download'}
                  </dd>
                  <p className="help mt-1">
                    Final page delivered, not confirmation the recipient saved it.
                  </p>
                </div>
                <div>
                  <dt className="text-muted-foreground">Last access</dt>
                  <dd className="mt-1">
                    {share.last_access_at ? date(share.last_access_at) : 'No access recorded'}
                  </dd>
                </div>
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
                <div>
                  <dt className="text-muted-foreground">Download limits</dt>
                  <dd className="mt-1">
                    {share.requests_per_minute} requests per minute ·{' '}
                    {share.max_concurrent_downloads} concurrent
                  </dd>
                  <p className="help mt-1">
                    Every page and HEAD request counts. Responses are limited to 20 seconds.
                  </p>
                </div>
              </dl>
              {share.scope === 'all' && (
                <p className="help">
                  Future sources are automatically included. Use selected sources if new sources
                  should require your approval.
                </p>
              )}
              <div className="field">
                <Label htmlFor={`share-url-${share.id}`}>Catalog URL (no password included)</Label>
                <Input
                  id={`share-url-${share.id}`}
                  readOnly
                  value={share.url}
                  placeholder="Available after HTTPS public origin is configured"
                  className="font-mono text-xs"
                />
                <CopyButton value={share.url} label="Copy catalog URL" />
              </div>
              <div className="toolbar">
                <Button variant="outline" size="sm" onClick={() => openEditor(share)}>
                  Edit permissions & limits
                </Button>
                <ConfirmAction
                  title={`${share.enabled ? 'Disable' : 'Enable'} “${share.name}”?`}
                  description={
                    share.enabled
                      ? 'New requests will be denied immediately. Existing downloaded copies cannot be recalled. You can enable this share again later.'
                      : `This password will grant read-only access to ${share.scope === 'all' ? 'all current and future sources' : 'the selected sources'}, limited to: ${share.fields.join(', ')}.`
                  }
                  label={share.enabled ? 'Disable access' : 'Enable access'}
                  trigger={
                    <Button
                      variant="outline"
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
                    await api<CatalogShare>(`/shares/${encodeURIComponent(share.id)}`, {
                      method: 'PUT',
                      body: JSON.stringify({ ...shareBody(share), enabled: !share.enabled }),
                    })
                    setNotice(
                      `Access ${share.enabled ? 'disabled' : 'enabled'} for “${share.name}”.`,
                    )
                    await refreshData()
                  }}
                />
                <ConfirmAction
                  title={`Rotate password for “${share.name}”?`}
                  description="The previous password will stop working immediately. Give the replacement to the recipient through a secure channel. Permissions and enabled state are unchanged."
                  label="Rotate password"
                  trigger={
                    <Button variant="outline" size="sm">
                      Rotate password
                    </Button>
                  }
                  action={async () => {
                    rememberFocus()
                    const result = await api<SharePassword>(
                      `/shares/${encodeURIComponent(share.id)}/rotate`,
                      { method: 'POST', body: JSON.stringify({ revision: share.revision }) },
                    )
                    setCredential(result)
                    await refreshData()
                  }}
                />
                <ConfirmAction
                  title={`Revoke “${share.name}” permanently?`}
                  description="This share and its password will stop working. The recipient’s existing downloaded data is not deleted. Create a new share if you want to grant access again."
                  label="Revoke share"
                  destructive
                  trigger={
                    <Button variant="ghost" size="sm" className="text-destructive">
                      Revoke
                    </Button>
                  }
                  action={async () => {
                    await api<void>(`/shares/${encodeURIComponent(share.id)}`, {
                      method: 'DELETE',
                      body: JSON.stringify({ revision: share.revision }),
                    })
                    setNotice(`Share “${share.name}” revoked.`)
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
            title="No catalog shares"
            description="Create a disabled share, choose its source permissions and safe fields, then enable it when you are ready."
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
              {editing === 'new' ? 'Create disabled share' : 'Edit share permissions & limits'}
            </DialogTitle>
            <DialogDescription>
              Only the selected published fields are exposed. Passwords never grant access to
              administrative APIs.
            </DialogDescription>
          </DialogHeader>
          {editing && shares.data && (
            <ShareForm
              key={editing === 'new' ? 'new' : editing.id}
              share={editing === 'new' ? undefined : editing}
              data={shares.data}
              onSaving={setSaving}
              onSaved={(result) => {
                setEditing(null)
                if (result) setCredential(result)
                else setNotice('Share permissions saved.')
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
  share,
  data,
  onSaving,
  onSaved,
}: {
  share?: CatalogShare
  data: CatalogShares
  onSaving: (saving: boolean) => void
  onSaved: (credential?: SharePassword) => void
}) {
  const [original, setOriginal] = useState(share)
  const [name, setName] = useState(share?.name ?? '')
  const [scope, setScope] = useState<CatalogShare['scope']>(share?.scope ?? 'selected')
  const [sourceIDs, setSourceIDs] = useState(share?.source_ids ?? [])
  const [fields, setFields] = useState(share?.fields ?? data.fields)
  const [expiresAt, setExpiresAt] = useState(localDateTime(share?.expires_at, 'seconds'))
  const [requestsPerMinute, setRequestsPerMinute] = useState(
    String(share?.requests_per_minute ?? 120),
  )
  const [maxConcurrentDownloads, setMaxConcurrentDownloads] = useState(
    String(share?.max_concurrent_downloads ?? 2),
  )
  const [acknowledged, setAcknowledged] = useState(false)
  const [reloaded, setReloaded] = useState(false)
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (scope === 'selected' && sourceIDs.length === 0)
        throw new Error('Select at least one source.')
      if (!fields.length) throw new Error('Select at least one safe field.')
      const requests = Number(requestsPerMinute)
      const concurrent = Number(maxConcurrentDownloads)
      if (!Number.isInteger(requests) || requests < 1 || requests > 3600)
        throw new Error('Requests per minute must be a whole number between 1 and 3600.')
      if (!Number.isInteger(concurrent) || concurrent < 1 || concurrent > 16)
        throw new Error('Concurrent downloads must be a whole number between 1 and 16.')
      if (expiresAt && !Number.isFinite(new Date(expiresAt).getTime()))
        throw new Error('Choose a valid expiration date and time.')
      const body = {
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
        await api<CatalogShare>(`/shares/${encodeURIComponent(original.id)}`, {
          method: 'PUT',
          body: JSON.stringify(body),
        })
        onSaved()
      } else {
        const result = await api<SharePassword>('/shares', {
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
      const latest = await api<CatalogShares>('/shares')
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
    name !== (original?.name ?? '') ||
    scope !== (original?.scope ?? 'selected') ||
    JSON.stringify(sourceIDs) !== JSON.stringify(original?.source_ids ?? []) ||
    JSON.stringify(fields) !== JSON.stringify(original?.fields ?? data.fields) ||
    expiresAt !== localDateTime(original?.expires_at, 'seconds') ||
    requestsPerMinute !== String(original?.requests_per_minute ?? 120) ||
    maxConcurrentDownloads !== String(original?.max_concurrent_downloads ?? 2) ||
    (!original && acknowledged)
  useUnsavedChanges(dirty, 'catalog sharing', busy)
  const unavailable = sourceIDs.filter((id) => !data.sources.some((source) => source.id === id))
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
          <Label htmlFor="share-name">Recipient / share name</Label>
          <Input
            id="share-name"
            required
            maxLength={120}
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
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
                No sources are available. Add a source before creating a share for selected sources.
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
              Access to all sources automatically includes sources added in the future, including
              imported catalogs that are published locally. New sources will not require another
              approval.
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
            No download URLs, magnets, tracker passkeys, arbitrary metadata or raw data. Original
            provenance accompanies authorized fields.
          </p>
        </fieldset>
        <fieldset className="space-y-4 border-t border-border pt-4">
          <legend className="px-1 text-sm font-medium">Access policy</legend>
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
              Uses the browser's timezone. Leave empty for no expiration. Expired passwords cannot
              download data, including with a saved cursor.
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
                required
                min="1"
                max="3600"
                step="1"
                value={requestsPerMinute}
                onChange={(event) => setRequestsPerMinute(event.target.value)}
                aria-describedby="share-limits-help"
              />
            </div>
            <div className="field">
              <Label htmlFor="share-concurrent-limit">Concurrent downloads</Label>
              <Input
                id="share-concurrent-limit"
                type="number"
                required
                min="1"
                max="16"
                step="1"
                value={maxConcurrentDownloads}
                onChange={(event) => setMaxConcurrentDownloads(event.target.value)}
                aria-describedby="share-limits-help"
              />
            </div>
          </div>
          <p id="share-limits-help" className="help">
            Limits apply across all recipients using this password and all server instances. Every
            authenticated page and HEAD request counts toward a rolling minute. Active responses
            finish or time out within 20 seconds. Clients exceeding a limit receive HTTP 429 with a
            retry delay.
          </p>
        </fieldset>
      </fieldset>
      <p className="help">
        {original
          ? `Access remains ${original.enabled ? 'enabled' : 'disabled'}, unless expired. Permission and policy changes apply to every new request, including saved cursors. Already downloaded copies cannot be recalled.`
          : 'Access starts disabled. Save the generated password securely; enable the share separately when ready.'}
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
            (scope === 'selected' ? sourceIDs.length === 0 : !acknowledged)
          }
        >
          {save.isPending ? 'Saving…' : original ? 'Save permissions' : 'Create disabled share'}
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
  return (
    <Dialog
      open
      unsavedChanges={{ dirty: true, label: 'catalog sharing password' }}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Save this password now</DialogTitle>
          <DialogDescription>
            This is the only time the password for “{credential.share.name}” is shown. Closing this
            dialog clears it from the page. The server cannot retrieve it later.
          </DialogDescription>
        </DialogHeader>
        <div className="field">
          <Label htmlFor="created-share-password">Sharing password</Label>
          <Input
            id="created-share-password"
            type={visible ? 'text' : 'password'}
            readOnly
            autoComplete="off"
            spellCheck={false}
            value={credential.password}
            className="font-mono text-xs"
          />
        </div>
        <div className="toolbar items-start">
          <Button variant="outline" onClick={() => setVisible(!visible)}>
            {visible ? 'Hide password' : 'Reveal password'}
          </Button>
          <CopyButton
            value={credential.password}
            label="Copy password"
            manualCopyMessage="Clipboard access is unavailable. Reveal the password and copy it manually."
            successMessage="Copied to your clipboard. Clear it after storing or sending the password securely."
          />
        </div>
        <p className="notice">
          {shareExpired(credential.share)
            ? 'This share has expired; update its expiration before downloading.'
            : credential.share.enabled
              ? 'This share is enabled.'
              : 'This share is disabled; enable it separately.'}{' '}
          Send the catalog URL and password through a secure channel. Never append the password to
          the URL.
        </p>
        {credential.share.url && (
          <div className="field">
            <Label htmlFor="created-share-url">Catalog URL</Label>
            <Input
              id="created-share-url"
              readOnly
              value={credential.share.url}
              className="font-mono text-xs"
            />
            <CopyButton value={credential.share.url} label="Copy catalog URL" />
          </div>
        )}
        <DialogFooter>
          <Button onClick={onClose}>I have saved the password</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
