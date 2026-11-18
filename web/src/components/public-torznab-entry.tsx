import { InfoTip } from '@/components/ui/info-tip'
import { LinkIcon, EraserIcon } from '@phosphor-icons/react'
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
import { ResultSorting } from '@/components/result-controls'
import {
  PublicAnnounceFields,
  clearPublicCredentials,
  type PublicInfo,
} from '@/components/public-connection-fields'

export function PublicTorznabEntry({ info }: { info: PublicInfo }) {
  const saved = usePublicCredential(info.credential_scope)
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
    const allowed = new Set((info.sources ?? info.private_sources ?? []).map((source) => source.id))
    const selected = options.sources ?? []
    const sources = selected.filter((id) => allowed.has(id))
    const sorting = options.sorting?.filter(
      (rule) => Boolean(info.sources) || rule.field !== 'source',
    )
    if (selected.length !== sources.length || sorting?.length !== options.sorting?.length) {
      setOptions({ ...options, sources, sorting })
    }
  }, [info.sources, info.private_sources, options])
  async function create(form: HTMLFormElement) {
    if (request.current) return
    const values = new FormData(form),
      announces: Record<string, string> = {}
    for (const source of info.private_sources ?? []) {
      const value = String(values.get(`announce:${source.id}`) ?? '').trim()
      if (value) announces[source.id] = value
    }
    const removedAnnounces = values
      .getAll('remove_announce')
      .map(String)
      .filter((id) => !announces[id])
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(undefined)
    setConnection(undefined)
    try {
      const result = await api<PublicCredential & { url: string; api_key: string }>(
        `/${info.trusted ? 'trusted' : 'public'}/torznab/install`,
        {
          method: 'POST',
          cache: 'no-store',
          body: JSON.stringify({
            options,
            announces,
            ...(saved ? { credential: saved.credential } : {}),
            ...(removedAnnounces.length ? { remove_announces: removedAnnounces } : {}),
          }),
          signal: controller.signal,
        },
      )
      if (
        !controller.signal.aborted &&
        isCurrentPublicCredential(saved?.credential, info.credential_scope)
      ) {
        connectionCredential.current = result.credential
        if (result.credential) savePublicCredential(result, info.credential_scope)
        else forgetPublicCredential(undefined, info.credential_scope)
        setConnection(result)
      }
    } catch (e) {
      if (
        !controller.signal.aborted &&
        isCurrentPublicCredential(saved?.credential, info.credential_scope)
      ) {
        if (e instanceof ApiError && e.status === 401)
          forgetPublicCredential(saved?.credential, info.credential_scope)
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
  const endpoint = connection ? new URL(connection.url, window.location.origin) : null
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
                sources={info.sources ?? info.private_sources ?? []}
                showSources={Boolean(info.sources)}
                id="public-torznab"
              />
            </Disclosure>
            <ResultSorting
              value={options.sorting}
              onChange={(sorting) => setOptions({ ...options, sorting })}
              sources={Boolean(info.sources)}
              defaultOrder="newest first"
            />
            <PublicAnnounceFields
              sources={info.private_sources ?? []}
              pending={pending}
              torznab
              scope={info.credential_scope}
            />
          </fieldset>
          <div className="flex items-start gap-1.5 text-sm text-muted-foreground">
            <p>
              {info.use_instance_announces
                ? 'Private downloads use the instance’s announce URLs.'
                : 'Private sources without your own announce URL are limited to known cached results.'}
            </p>
            <InfoTip label="Cache services">
              Availability uses the shared cache already known by this instance. Configure debrid
              accounts in your download client.
            </InfoTip>
          </div>
          <ErrorNotice error={error} />
          {preferenceError && (
            <p className="text-sm text-destructive" role="alert">
              {preferenceError}
            </p>
          )}
          <div className="flex flex-wrap gap-3">
            <Button type="submit" disabled={pending || Boolean(preferenceError)}>
              <LinkIcon aria-hidden="true" />
              {pending ? 'Creating connection…' : 'Create connection link'}
            </Button>
            {saved && (
              <Button
                type="button"
                variant="warning"
                disabled={pending}
                onClick={() => {
                  if (formRef.current) clearPublicCredentials(formRef.current)
                  forgetPublicCredential(undefined, info.credential_scope)
                  setError(undefined)
                }}
              >
                <EraserIcon aria-hidden="true" />
                Forget saved credentials
              </Button>
            )}
          </div>
        </form>
      </section>
      {connection && !preferenceError && (
        <section className="section-card">
          <div className="flex min-w-0 items-center gap-1.5">
            <h2 className="section-title">Connect your client</h2>
            <InfoTip label="Connect your client">
              The connection key carries your URL filters and encrypted personal announce URLs.
              Changing filters updates this link without changing existing connections. No visitor
              profile is saved.
            </InfoTip>
          </div>
          <p className="help">
            In Prowlarr, choose Generic Torznab and enter these values. Keep the API key private.
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
              revealLabel="API key"
              autoComplete="off"
              readOnly
              value={credential}
            />
            <CopyButton
              value={credential}
              label="Copy API key"
              manualCopyMessage="Clipboard access is unavailable. Reveal the API key and copy it manually."
              successMessage="Copied to your clipboard. Keep it private."
            />
          </div>
          <Disclosure title="Full endpoint URL">
            <div className="field">
              <Label htmlFor="public-torznab-full">Endpoint URL</Label>
              <Input
                id="public-torznab-full"
                type="password"
                revealLabel="endpoint URL"
                autoComplete="off"
                spellCheck={false}
                readOnly
                value={full}
              />
              <CopyButton
                value={full}
                label="Copy endpoint URL"
                manualCopyMessage="Clipboard access is unavailable. Reveal the endpoint URL and copy it manually."
                successMessage="Copied to your clipboard. Keep it private."
              />
            </div>
          </Disclosure>
        </section>
      )}
    </div>
  )
}
