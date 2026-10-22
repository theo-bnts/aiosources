import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import type { IncomingNetwork, NetworkSettings, PublicLimits } from '@/lib/network-types'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useNetworkSettings, useSaveNetwork } from '@/lib/network-settings'

const publicLimitFields = [
  ['setup_per_minute', 'Link creations per minute', 100000, 12],
  ['requests_per_minute', 'Public requests per minute', 100000, 120],
  ['concurrent', 'Simultaneous public requests', 1000, 50],
] as const

type LimitDrafts = Record<keyof PublicLimits, { enabled: boolean; value: string }>

function draftLimits(limits: PublicLimits): LimitDrafts {
  return Object.fromEntries(
    publicLimitFields.map(([field, , , fallback]) => [
      field,
      { enabled: limits[field] > 0, value: String(limits[field] || fallback) },
    ]),
  ) as LimitDrafts
}

function limitValues(limits: LimitDrafts): PublicLimits {
  return Object.fromEntries(
    publicLimitFields.map(([field]) => [
      field,
      limits[field].enabled ? Number(limits[field].value) : 0,
    ]),
  ) as PublicLimits
}

export default function IncomingNetworkPage() {
  return <IncomingSettings section="proxy" />
}

export function PublicRequestLimitsPage() {
  return <IncomingSettings section="limits" />
}

function IncomingSettings({ section }: { section: 'proxy' | 'limits' }) {
  const proxy = section === 'proxy'
  const query = useNetworkSettings()
  const status = useQuery({
    queryKey: ['network-incoming-status'],
    enabled: proxy,
    queryFn: ({ signal }) =>
      api<{ connection_ip: string; visitor_ip: string }>('/settings/network/incoming', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const [draft, setDraft] = useState<{
    base: NetworkSettings
    value: IncomingNetwork
    trusted: string
    limits: LimitDrafts
  } | null>(null)
  const save = useSaveNetwork(() => setDraft(null))
  const limits = draft?.limits ?? (query.data && draftLimits(query.data.public_limits))
  const current = draft?.value ?? query.data?.incoming
  const trusted = draft?.trusted ?? current?.trusted_proxies.join('\n') ?? ''
  const value = current && {
    ...current,
    trusted_proxies: trusted
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean),
  }
  const dirty = Boolean(
    draft &&
    (proxy
      ? JSON.stringify(value) !== JSON.stringify(query.data?.incoming)
      : publicLimitFields.some(([field]) => {
          const limit = draft.limits[field]
          const saved = query.data?.public_limits[field]
          return (
            limit.enabled !== (saved !== undefined && saved > 0) ||
            (limit.enabled && limit.value !== String(saved))
          )
        })),
  )
  const conflict =
    Boolean(draft && draft.base.revision !== query.data?.revision) ||
    (save.error instanceof ApiError && save.error.status === 409)
  useUnsavedChanges(
    dirty,
    proxy ? 'incoming proxy settings' : 'public request limits',
    save.isPending,
  )
  function change(change: Partial<IncomingNetwork>, text?: string, publicLimits = limits!) {
    if (!query.data || !current) return
    setDraft({
      base: draft?.base ?? query.data,
      value: { ...current, ...change },
      trusted: text ?? trusted,
      limits: publicLimits,
    })
    save.reset()
  }
  return (
    <div className="page">
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {proxy && <ErrorNotice error={status.error} retry={() => void status.refetch()} />}
      {query.isPending ? (
        <Loading label={proxy ? 'Loading incoming proxy settings' : 'Loading request limits'} />
      ) : (
        current &&
        value &&
        limits && (
          <form
            className="space-y-5"
            aria-label={proxy ? 'Incoming proxy settings' : 'Request limits'}
            onSubmit={(event) => {
              event.preventDefault()
              if (dirty && draft && !conflict && !save.isPending)
                save.mutate({
                  ...draft.base,
                  ...(proxy ? { incoming: value } : { public_limits: limitValues(limits) }),
                })
            }}
          >
            {proxy ? (
              <section className="section-card" aria-label="Trusted proxy">
                <h2 className="section-title">Trusted proxy</h2>
                <div className="flex items-center gap-3">
                  <Switch
                    id="incoming-enabled"
                    checked={current.enabled}
                    disabled={save.isPending}
                    onCheckedChange={(enabled) => change({ enabled })}
                  />
                  <Label htmlFor="incoming-enabled">Trust an incoming proxy header</Label>
                </div>
                <p className="help">
                  Use the visitor’s IP for public request limits and sign-in protection. Only
                  headers received from the trusted proxies below are accepted.
                </p>
                <div className="grid gap-5 md:grid-cols-2">
                  <div className="field min-w-0">
                    <Label htmlFor="incoming-header">IP address header</Label>
                    <Input
                      id="incoming-header"
                      list="incoming-header-options"
                      required
                      maxLength={128}
                      value={current.ip_header}
                      disabled={save.isPending}
                      placeholder="CF-Connecting-IP"
                      onChange={(event) => {
                        const ip_header = event.target.value
                        const common = ip_header.toLowerCase()
                        change({
                          ip_header,
                          ...(['cf-connecting-ip', 'x-real-ip'].includes(common)
                            ? { format: 'single' as const }
                            : common === 'x-forwarded-for'
                              ? { format: 'chain' as const }
                              : {}),
                        })
                      }}
                    />
                    <datalist id="incoming-header-options">
                      <option value="CF-Connecting-IP" />
                      <option value="X-Real-IP" />
                      <option value="X-Forwarded-For" />
                    </datalist>
                  </div>
                  <div className="field min-w-0">
                    <Label htmlFor="incoming-format">Header format</Label>
                    <select
                      id="incoming-format"
                      className="native-select w-full"
                      value={current.format}
                      disabled={save.isPending}
                      onChange={(event) =>
                        change({ format: event.target.value as IncomingNetwork['format'] })
                      }
                    >
                      <option value="single">Single IP address</option>
                      <option value="chain">Comma-separated IP chain</option>
                    </select>
                  </div>
                </div>
                <div className="field">
                  <Label htmlFor="incoming-proxies">Trusted proxy addresses</Label>
                  <Textarea
                    id="incoming-proxies"
                    value={trusted}
                    disabled={save.isPending}
                    rows={5}
                    required={current.enabled}
                    placeholder={'192.0.2.10\n10.0.0.0/24'}
                    onChange={(event) => change({}, event.target.value)}
                  />
                  <div className="space-y-2">
                    {status.data && (
                      <p className="help break-words">
                        Direct connection address:{' '}
                        <span className="mono">{status.data.connection_ip}</span>. Visitor address
                        using saved settings: <span className="mono">{status.data.visitor_ip}</span>
                        .
                      </p>
                    )}
                    <p className="help">
                      One IP address or CIDR network per line. For a tunnel, enter the cloudflared
                      address seen by AIOSources. For Cloudflare’s standard proxy, use its{' '}
                      <a
                        className="link"
                        href="https://www.cloudflare.com/ips/"
                        target="_blank"
                        rel="noreferrer"
                      >
                        published IP ranges
                      </a>
                      . Trust only proxies you control or explicitly authorize.
                    </p>
                    <p className="help">
                      Cloudflare uses CF-Connecting-IP with a single address. X-Forwarded-For chains
                      are read from the nearest proxy back to the first untrusted address. Missing
                      or invalid headers use the direct connection address. IPv4 and IPv6 are
                      supported.
                    </p>
                  </div>
                </div>
              </section>
            ) : (
              <section className="section-card" aria-label="Public request limits">
                <h2 className="section-title">Public request limits</h2>
                <div className="grid gap-5 md:grid-cols-3">
                  {publicLimitFields.map(([field, label, max]) => (
                    <div className="field min-w-0" key={field}>
                      <div className="flex items-center justify-between gap-3">
                        <Label htmlFor={`public-limit-${field}`}>{label}</Label>
                        <Switch
                          aria-label={`${label} limit`}
                          checked={limits[field].enabled}
                          disabled={save.isPending}
                          onCheckedChange={(enabled) =>
                            change({}, undefined, {
                              ...limits,
                              [field]: { ...limits[field], enabled },
                            })
                          }
                        />
                      </div>
                      <Input
                        id={`public-limit-${field}`}
                        type="number"
                        min={1}
                        max={max}
                        step={1}
                        required
                        disabled={save.isPending || !limits[field].enabled}
                        value={limits[field].enabled ? limits[field].value : ''}
                        placeholder={limits[field].enabled ? undefined : 'Unlimited'}
                        onChange={(event) =>
                          change({}, undefined, {
                            ...limits,
                            [field]: { ...limits[field], value: event.target.value },
                          })
                        }
                      />
                    </div>
                  ))}
                </div>
                <div className="space-y-2">
                  <p className="help">
                    Switch off any limit to remove that restriction. Per-minute limits apply to each
                    visitor IP and are shared by Search, Stremio and Torznab. Simultaneous requests
                    are limited across this application process. Use the visitor IP settings in
                    Network → Incoming when behind a reverse proxy.
                  </p>
                  <p className="help">
                    Changes apply without restarting or replacing installation links. Lowering
                    concurrency lets active requests finish before admitting new ones.
                  </p>
                </div>
              </section>
            )}
            {conflict && (
              <p className="notice notice-warning" role="status">
                Network settings changed. Your draft is preserved; discard it to load the saved
                settings.
              </p>
            )}
            <ErrorNotice error={save.error} />
            <div className="toolbar">
              <Button type="submit" disabled={!dirty || conflict || save.isPending}>
                {save.isPending ? 'Saving…' : 'Save'}
              </Button>
              <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
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
            </div>
          </form>
        )
      )}
      {proxy && <RuntimeSettingsPanel section="connections" />}
    </div>
  )
}
