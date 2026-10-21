import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { CheckCircleIcon, PencilSimpleIcon, PlusIcon, TrashIcon } from '@phosphor-icons/react'
import { api, ApiError, ConnectionError } from '@/lib/api'
import { useNotification } from '@/lib/notifications'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import type { NetworkProxy, NetworkSettings } from '@/lib/network-types'
import { useNetworkSettings, useSaveNetwork } from '@/lib/network-settings'
import type { Items, ProviderSummary } from '@/lib/types'
import { ConfirmAction, Empty, ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { SecretPicker } from '@/components/secret-picker'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

function proxyInUse(settings: NetworkSettings, id: string) {
  return settings.default === id || Object.values(settings.overrides).includes(id)
}

export function NetworkPage() {
  const query = useNetworkSettings()
  const save = useSaveNetwork()
  const notify = useNotification()
  const [editing, setEditing] = useState<{ proxy: NetworkProxy; settings: NetworkSettings } | null>(
    null,
  )
  const probe = useMutation({
    mutationFn: (id: string) =>
      api<{ exit_ip: string; warp: string; duration_ms: number }>('/settings/network/test', {
        method: 'POST',
        body: JSON.stringify({ id }),
      }),
    onSuccess: (result) =>
      notify(
        `Proxy connection verified. Exit IP: ${result.exit_ip}${['on', 'plus'].includes(result.warp) ? ' · WARP connected' : ''}.`,
      ),
    onError: (error) => {
      if (!(error instanceof ConnectionError)) notify({ message: error.message, warning: true })
    },
  })
  const settings = query.data
  return (
    <div className="page">
      <div className="flex justify-end">
        <Button
          disabled={!settings || save.isPending || settings.proxies.length >= 32}
          onClick={() => {
            if (settings)
              setEditing({
                settings,
                proxy: { id: `proxy-${crypto.randomUUID()}`, name: '', url: '', enabled: false },
              })
          }}
        >
          <PlusIcon />
          Add proxy
        </Button>
      </div>
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      <ErrorNotice error={save.error} retry={() => void query.refetch().then(() => save.reset())} />
      {query.isPending ? (
        <Loading label="Loading proxies" />
      ) : (
        settings &&
        (settings.proxies.length ? (
          <div className="space-y-4">
            {settings.proxies.map((proxy) => {
              const used = proxyInUse(settings, proxy.id)
              return (
                <section key={proxy.id} className="section-card" aria-label={`Proxy ${proxy.name}`}>
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <h2 className="section-title break-words">{proxy.name}</h2>
                      <p className="mono mt-1 break-all text-muted-foreground">{proxy.url}</p>
                    </div>
                    <Switch
                      checked={proxy.enabled}
                      aria-label={`${proxy.enabled ? 'Disable' : 'Enable'} ${proxy.name}`}
                      disabled={save.isPending}
                      onCheckedChange={(enabled) =>
                        save.mutate({
                          ...settings,
                          proxies: settings.proxies.map((item) =>
                            item.id === proxy.id ? { ...item, enabled } : item,
                          ),
                        })
                      }
                    />
                  </div>
                  {used && !proxy.enabled && (
                    <p role="status" className="notice notice-warning">
                      Connections assigned to this proxy are stopped until it is enabled or their
                      routing is changed.
                    </p>
                  )}
                  <div className="toolbar">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={probe.isPending || save.isPending}
                      onClick={() => probe.mutate(proxy.id)}
                    >
                      <CheckCircleIcon />
                      {probe.isPending && probe.variables === proxy.id
                        ? 'Testing…'
                        : 'Test connection'}
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={save.isPending}
                      onClick={() => setEditing({ proxy, settings })}
                    >
                      <PencilSimpleIcon />
                      Edit
                    </Button>
                    <ConfirmAction
                      title={`Delete proxy “${proxy.name}”?`}
                      description="The proxy profile will be removed. Stored secrets are kept."
                      label="Delete proxy"
                      variant="destructive"
                      trigger={
                        <Button
                          size="sm"
                          variant="destructive"
                          disabled={save.isPending || used}
                          title={
                            used
                              ? 'Change its routing assignments before deleting this proxy.'
                              : undefined
                          }
                        >
                          <TrashIcon />
                          Delete
                        </Button>
                      }
                      action={async () => {
                        await save.mutateAsync({
                          ...settings,
                          proxies: settings.proxies.filter((item) => item.id !== proxy.id),
                        })
                      }}
                    />
                  </div>
                  {used && (
                    <p className="help">
                      Assigned in Routing. Change those assignments before deleting this proxy.
                    </p>
                  )}
                </section>
              )
            })}
          </div>
        ) : (
          <Empty
            title="No proxies configured"
            description="Add a WARP, VPN or other HTTP(S) or SOCKS5 proxy, then choose where to use it in Routing."
          />
        ))
      )}
      {editing && (
        <ProxyEditor
          initial={editing.proxy}
          snapshot={editing.settings}
          onClose={() => setEditing(null)}
        />
      )}
    </div>
  )
}

function ProxyEditor({
  initial,
  snapshot,
  onClose,
}: {
  initial: NetworkProxy
  snapshot: NetworkSettings
  onClose: () => void
}) {
  const query = useNetworkSettings()
  const [base, setBase] = useState(snapshot)
  const [draft, setDraft] = useState(initial)
  const save = useSaveNetwork(onClose)
  const existing = base.proxies.some((proxy) => proxy.id === draft.id)
  const dirty =
    JSON.stringify(draft) !==
    JSON.stringify(base.proxies.find((proxy) => proxy.id === draft.id) ?? initial)
  const conflict =
    Boolean(query.data && query.data.revision !== base.revision) ||
    (save.error instanceof ApiError && save.error.status === 409)
  function update(change: Partial<NetworkProxy>) {
    setDraft((current) => {
      const next = { ...current, ...change }
      if (!next.username_ref) delete next.username_ref
      if (!next.password_ref) delete next.password_ref
      return next
    })
  }
  return (
    <Dialog
      open
      unsavedChanges={{ dirty, label: 'proxy settings', pending: save.isPending }}
      onOpenChange={(open) => {
        if (!open && !save.isPending) onClose()
      }}
    >
      <DialogContent showCloseButton={!save.isPending}>
        <DialogHeader>
          <DialogTitle>{existing ? 'Edit proxy' : 'Add proxy'}</DialogTitle>
          <DialogDescription>
            Connect to an existing HTTP(S) or SOCKS5 proxy, including WARP local proxy mode.
          </DialogDescription>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            if ((!existing || dirty) && !conflict && !save.isPending)
              save.mutate({
                ...base,
                proxies: existing
                  ? base.proxies.map((item) => (item.id === draft.id ? draft : item))
                  : [...base.proxies, draft],
              })
          }}
        >
          <div className="field">
            <Label htmlFor="proxy-name">Name</Label>
            <Input
              id="proxy-name"
              required
              maxLength={160}
              placeholder="Cloudflare WARP"
              value={draft.name}
              disabled={save.isPending}
              onChange={(event) => update({ name: event.target.value })}
            />
          </div>
          <div className="field">
            <Label htmlFor="proxy-url">Proxy URL</Label>
            <Input
              id="proxy-url"
              required
              maxLength={2048}
              placeholder="socks5://warp:40000"
              value={draft.url}
              autoComplete="off"
              spellCheck={false}
              disabled={save.isPending}
              onChange={(event) => update({ url: event.target.value })}
            />
            <p className="help">
              Use an address reachable from AIOSources. Keep credentials in Secrets.
            </p>
          </div>
          <Disclosure title="Authentication">
            <SecretPicker
              label="Username"
              value={draft.username_ref ?? ''}
              onChange={(username_ref) => update({ username_ref })}
              prefix="proxy_username"
              addLabel="Add username"
              valueLabel="Proxy username"
              disabled={save.isPending}
            />
            <SecretPicker
              label="Password"
              value={draft.password_ref ?? ''}
              onChange={(password_ref) => update({ password_ref })}
              prefix="proxy_password"
              addLabel="Add password"
              valueLabel="Proxy password"
              disabled={save.isPending}
            />
            <p className="help">Leave both empty if this proxy does not require authentication.</p>
          </Disclosure>
          {conflict && (
            <div className="notice notice-warning space-y-3">
              <p>
                Network settings changed. Your draft is preserved; reload the saved settings before
                saving.
              </p>
              <Button
                type="button"
                variant="outline"
                disabled={save.isPending || !query.data}
                onClick={() => {
                  if (!query.data) return
                  const current = query.data.proxies.find((proxy) => proxy.id === draft.id)
                  if (existing && !current) {
                    onClose()
                    return
                  }
                  setBase(query.data)
                  setDraft(current ?? initial)
                  save.reset()
                }}
              >
                Discard changes and reload
              </Button>
            </div>
          )}
          <ErrorNotice error={save.error} />
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={save.isPending}>
                Back
              </Button>
            </DialogClose>
            <Button type="submit" disabled={save.isPending || conflict || (existing && !dirty)}>
              {save.isPending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ConnectionSelect({
  id,
  label,
  value,
  settings,
  onChange,
  inherit,
  disabled,
}: {
  id: string
  label: string
  value: string
  settings: NetworkSettings
  onChange: (value: string) => void
  inherit?: string
  disabled?: boolean
}) {
  return (
    <div className="field min-w-0">
      <Label htmlFor={id} className="break-words">
        {label}
      </Label>
      <select
        id={id}
        className="native-select min-w-0 w-full"
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      >
        {inherit && <option value="">{inherit}</option>}
        <option value="system">System connection</option>
        <option value="direct">Direct connection</option>
        {settings.proxies.map((proxy) => (
          <option key={proxy.id} value={proxy.id} disabled={!proxy.enabled}>
            {proxy.name}
            {!proxy.enabled ? ' · disabled' : ''}
          </option>
        ))}
      </select>
    </div>
  )
}

export function NetworkRoutingPage() {
  const query = useNetworkSettings()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const [draft, setDraft] = useState<NetworkSettings | null>(null)
  const save = useSaveNetwork(() => setDraft(null))
  const settings = draft ?? query.data
  const sources = providers.data?.items ?? []
  const dirty = Boolean(
    draft &&
    query.data &&
    (draft.default !== query.data.default ||
      [...new Set([...Object.keys(draft.overrides), ...Object.keys(query.data.overrides)])].some(
        (key) => draft.overrides[key] !== query.data?.overrides[key],
      )),
  )
  const conflict =
    Boolean(draft && query.data && draft.revision !== query.data.revision) ||
    (save.error instanceof ApiError && save.error.status === 409)
  useUnsavedChanges(dirty, 'network routing', save.isPending)
  function route(target: string, value: string) {
    if (!settings) return
    const overrides = { ...settings.overrides }
    if (value) overrides[target] = value
    else delete overrides[target]
    setDraft({ ...settings, overrides })
  }
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      {query.isPending ? (
        <Loading label="Loading network routing" />
      ) : (
        settings && (
          <form
            className="space-y-5"
            onSubmit={(event) => {
              event.preventDefault()
              if (dirty && !conflict && !save.isPending) save.mutate(settings)
            }}
          >
            <section className="section-card">
              <h2 className="section-title">Default connection</h2>
              <div className="max-w-xl">
                <ConnectionSelect
                  id="network-default"
                  label="Outgoing connections"
                  value={settings.default}
                  settings={settings}
                  disabled={save.isPending}
                  onChange={(value) => setDraft({ ...settings, default: value })}
                />
              </div>
              <p className="help">
                Applies to sources, metadata, debrid services and browser downloads unless
                overridden below. System connection preserves existing system proxy settings.
              </p>
            </section>
            <section className="section-card">
              <h2 className="section-title">Sources</h2>
              {providers.isPending ? (
                <Loading label="Loading sources" />
              ) : sources.length ? (
                <div className="grid gap-5 md:grid-cols-2">
                  {sources.map((source) => (
                    <ConnectionSelect
                      key={source.id}
                      id={`network-source-${source.id}`}
                      label={source.name || source.id}
                      value={settings.overrides[`source:${source.id}`] ?? ''}
                      settings={settings}
                      inherit="Use default connection"
                      disabled={save.isPending}
                      onChange={(value) => route(`source:${source.id}`, value)}
                    />
                  ))}
                </div>
              ) : (
                !providers.isError && (
                  <p className="help">
                    No sources configured. New sources use the default connection.
                  </p>
                )
              )}
            </section>
            <section className="section-card">
              <h2 className="section-title">Metadata</h2>
              <div className="grid gap-5 md:grid-cols-2">
                <ConnectionSelect
                  id="network-metadata:tmdb"
                  label="TMDB"
                  value={settings.overrides['metadata:tmdb'] ?? ''}
                  settings={settings}
                  inherit="Use default connection"
                  disabled={save.isPending}
                  onChange={(value) => route('metadata:tmdb', value)}
                />
              </div>
            </section>
            <section className="section-card">
              <h2 className="section-title">Debrid</h2>
              <div className="grid gap-5 md:grid-cols-2">
                {[
                  ['debrid:alldebrid', 'AllDebrid'],
                  ['debrid:torbox', 'TorBox'],
                  ['downloads', 'Browser downloads'],
                ].map(([target, label]) => (
                  <ConnectionSelect
                    key={target}
                    id={`network-${target}`}
                    label={label}
                    value={settings.overrides[target] ?? ''}
                    settings={settings}
                    inherit={
                      target === 'downloads'
                        ? 'Use debrid connection or default'
                        : 'Use default connection'
                    }
                    disabled={save.isPending}
                    onChange={(value) => route(target, value)}
                  />
                ))}
              </div>
              <p className="help">
                Browser downloads inherit their debrid service’s connection unless overridden.
                Stremio playback links are opened by your player.
              </p>
            </section>
            {settings.proxies.some((proxy) => !proxy.enabled && proxyInUse(settings, proxy.id)) && (
              <p role="status" className="notice notice-warning">
                Some connections use a disabled proxy. Enable it in Proxies or choose another
                connection.
              </p>
            )}
            {conflict && (
              <p role="status" className="notice notice-warning">
                Network settings changed. Your draft is preserved; discard it to load the saved
                settings.
              </p>
            )}
            <ErrorNotice error={save.error} />
            <div className="toolbar justify-between">
              <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
              <div className="flex gap-2">
                {conflict && (
                  <Button
                    type="button"
                    variant="warning"
                    disabled={save.isPending}
                    onClick={() => {
                      setDraft(null)
                      save.reset()
                    }}
                  >
                    Discard changes and reload
                  </Button>
                )}
                <Button type="submit" disabled={save.isPending || !dirty || conflict}>
                  {save.isPending ? 'Saving…' : 'Save'}
                </Button>
              </div>
            </div>
          </form>
        )
      )}
    </div>
  )
}
