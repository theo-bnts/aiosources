import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import {
  usePublicCredential,
  savePublicCredential,
  forgetPublicCredential,
  isCurrentPublicCredential,
  type PublicCredential,
} from '@/lib/public-credentials'
import { torznabPreferenceKey, torznabPreferenceError } from '@/lib/torznab-preferences'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { CopyButton } from '@/components/copy-button'
import { Disclosure } from '@/components/ui/disclosure'
import { ErrorNotice } from '@/components/common'
import { TorznabFilters, type TorznabOptions } from '@/components/torznab-filters'
import {
  PublicAnnounceFields,
  clearPublicCredentials,
  type PublicInfo,
} from '@/components/public-connection-fields'

export function PublicTorznabEntry({ info }: { info: PublicInfo }) {
  const saved = usePublicCredential()
  const [options, setOptions] = useState<TorznabOptions>({})
  const [connection, setConnection] = useState<{ url: string; api_key: string }>()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const request = useRef<AbortController | null>(null)
  const connectionCredential = useRef('')
  const formRef = useRef<HTMLFormElement>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if ((saved?.credential ?? '') !== connectionCredential.current) setConnection(undefined)
  }, [saved])
  useEffect(() => {
    if (!info.torznab_enabled) {
      setConnection(undefined)
      request.current?.abort()
      setPending(false)
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
      const result = await api<PublicCredential & { url: string; api_key: string }>(
        '/public/torznab/install',
        {
          method: 'POST',
          cache: 'no-store',
          body: JSON.stringify({
            options,
            announces,
            ...(saved ? { credential: saved.credential } : {}),
            ...(values.getAll('remove_announce').length
              ? { remove_announces: values.getAll('remove_announce') }
              : {}),
          }),
          signal: controller.signal,
        },
      )
      if (!controller.signal.aborted && isCurrentPublicCredential(saved?.credential)) {
        connectionCredential.current = result.credential
        if (result.credential) savePublicCredential(result)
        else forgetPublicCredential()
        setConnection(result)
      }
    } catch (e) {
      if (!controller.signal.aborted && isCurrentPublicCredential(saved?.credential)) {
        if (e instanceof ApiError && e.status === 401) forgetPublicCredential(saved?.credential)
        setError(e)
      }
    } finally {
      for (const key of Object.keys(announces)) delete announces[key]
      for (const key of [...values.keys()]) values.delete(key)
      clearPublicCredentials(form)
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  const credential = connection ? torznabPreferenceKey(connection.api_key, options) : ''
  const preferenceError = torznabPreferenceError(connection?.api_key ?? '', options)
  const endpoint = connection ? new URL(connection.url) : null
  const full = connection ? `${connection.url}?apikey=${encodeURIComponent(credential)}` : ''
  return (
    <div className="page max-w-4xl">
      <section className="section-card">
        <h2 className="section-title">Create a Torznab connection</h2>
        <form
          ref={formRef}
          className="space-y-5"
          onSubmit={(e) => {
            e.preventDefault()
            void create(e.currentTarget)
          }}
        >
          <fieldset disabled={pending} className="space-y-5">
            <Disclosure title="Filters">
              <TorznabFilters
                value={options}
                onChange={setOptions}
                sources={info.sources ?? []}
                id="public-torznab"
              />
            </Disclosure>
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
          <div className="flex flex-wrap gap-3">
            <Button type="submit" disabled={pending || Boolean(preferenceError)}>
              {pending ? 'Creating connection…' : 'Create connection link'}
            </Button>
            {saved && (
              <Button
                type="button"
                variant="warning"
                disabled={pending}
                onClick={() => {
                  if (formRef.current) clearPublicCredentials(formRef.current)
                  forgetPublicCredential()
                  setError(undefined)
                }}
              >
                Forget saved credentials
              </Button>
            )}
          </div>
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
