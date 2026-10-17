import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import { torznabPreferenceKey, torznabPreferenceError } from '@/lib/torznab-preferences'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { CopyButton } from '@/components/copy-button'
import { Disclosure } from '@/components/ui/disclosure'
import { Empty, ErrorNotice } from '@/components/common'
import { TorznabFilters, type TorznabOptions } from '@/components/torznab-filters'
import { PublicAnnounceFields, type PublicInfo } from '@/components/public-connection-fields'

export function PublicTorznabEntry({ info }: { info: PublicInfo }) {
  const [options, setOptions] = useState<TorznabOptions>({})
  const [connection, setConnection] = useState<{ url: string; api_key: string }>()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if (!info.torznab_enabled) {
      setConnection(undefined)
      request.current?.abort()
    }
  }, [info.torznab_enabled])
  useEffect(() => {
    const allowed = new Set((info.sources ?? []).map((source) => source.id))
    const selected = options.sources ?? []
    const sources = selected.filter((id) => allowed.has(id))
    if (selected.length !== sources.length) {
      setOptions({ ...options, sources })
    }
  }, [info.sources, options])
  async function create(form: HTMLFormElement) {
    if (request.current) return
    const values = new FormData(form),
      announces: Record<string, string> = {}
    for (const source of info.private_sources ?? []) {
      const value = String(values.get(`announce:${source.id}`) ?? '').trim()
      if (value) announces[source.id] = value
    }
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(undefined)
    setConnection(undefined)
    try {
      const result = await api<{ url: string; api_key: string }>('/public/torznab/install', {
        method: 'POST',
        cache: 'no-store',
        body: JSON.stringify({ options, announces }),
        signal: controller.signal,
      })
      if (!controller.signal.aborted) setConnection(result)
    } catch (e) {
      if (!controller.signal.aborted) setError(e)
    } finally {
      for (const key of Object.keys(announces)) delete announces[key]
      for (const key of [...values.keys()]) values.delete(key)
      form.reset()
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  if (!info.torznab_enabled)
    return (
      <div className="page">
        <Empty
          title="Public Torznab is unavailable"
          description="The administrator has not enabled this connection."
        />
      </div>
    )
  const credential = connection ? torznabPreferenceKey(connection.api_key, options) : ''
  const preferenceError = torznabPreferenceError(connection?.api_key ?? '', options)
  const endpoint = connection ? new URL(connection.url) : null
  const full = connection ? `${connection.url}?apikey=${encodeURIComponent(credential)}` : ''
  return (
    <div className="page max-w-4xl">
      <section className="section-card">
        <h2 className="section-title">Create a Torznab connection</h2>
        <form
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault()
            void create(e.currentTarget)
          }}
        >
          <fieldset disabled={pending} className="space-y-5">
            <TorznabFilters
              value={options}
              onChange={setOptions}
              sources={info.sources ?? []}
              id="public-torznab"
            />
            <PublicAnnounceFields sources={info.private_sources ?? []} pending={pending} torznab />
          </fieldset>
          <p className="help">
            Availability uses the shared cache already known by this instance. Configure debrid
            accounts in your download client. Private sources without your own announce URL are
            limited to known cached results.
          </p>
          <ErrorNotice error={error} />
          {preferenceError && (
            <p className="text-sm text-destructive" role="alert">
              {preferenceError}
            </p>
          )}
          <Button type="submit" disabled={pending || Boolean(preferenceError)}>
            {pending ? 'Creating connection…' : 'Create connection link'}
          </Button>
        </form>
      </section>
      {connection && !preferenceError && (
        <section className="section-card">
          <h2 className="section-title">Connect your client</h2>
          <p className="help">
            In Prowlarr, choose Generic Torznab. The connection key carries your URL filters and
            encrypted personal announce URLs; keep it private. Changing filters updates this link
            without changing existing connections. No visitor profile is saved.
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="field">
              <Label htmlFor="public-torznab-url">URL</Label>
              <Input id="public-torznab-url" readOnly value={endpoint?.origin ?? ''} />
              <CopyButton value={endpoint?.origin ?? ''} label="Copy URL" />
            </div>
            <div className="field">
              <Label htmlFor="public-torznab-path">API path</Label>
              <Input id="public-torznab-path" readOnly value={endpoint?.pathname ?? ''} />
              <CopyButton value={endpoint?.pathname ?? ''} label="Copy API path" />
            </div>
          </div>
          <div className="field">
            <Label htmlFor="public-torznab-key">API key</Label>
            <Input
              id="public-torznab-key"
              type="password"
              autoComplete="off"
              readOnly
              value={credential}
            />
            <CopyButton value={credential} label="Copy API key" />
          </div>
          <Disclosure title="Full endpoint URL">
            <div className="field">
              <Label htmlFor="public-torznab-full">Endpoint URL</Label>
              <Input id="public-torznab-full" readOnly value={full} />
              <CopyButton value={full} label="Copy endpoint URL" />
            </div>
          </Disclosure>
        </section>
      )}
    </div>
  )
}
