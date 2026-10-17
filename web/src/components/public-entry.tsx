import { LanguageFilter } from '@/components/language-filter'
import {
  useLanguagePreferences,
  resolvedLanguage,
  preferenceManifest,
  preferenceManifestError,
} from '@/lib/language-preferences'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useLocation } from 'react-router-dom'
import { api, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice, Loading } from '@/components/common'
import { CopyButton } from '@/components/copy-button'
import { Login } from '@/components/login'
import {
  PublicAnnounceFields,
  clearPublicCredentials,
  type PublicInfo,
} from '@/components/public-connection-fields'
import { PublicTorznabEntry } from '@/components/public-torznab-entry'
import { InstallationFilters } from '@/components/installation-filters'
import type { InstallationOptions } from '@/lib/language-preferences'

export function PublicEntry({
  layout = (content) => content,
}: {
  layout?: (content: ReactNode) => ReactNode
}) {
  const location = useLocation()
  const [preferences, setPreferences] = useLanguagePreferences()
  const [installationOptions, setInstallationOptions] = useState<InstallationOptions>({})
  const info = useQuery({
    queryKey: ['public-config'],
    queryFn: ({ signal }) => api<PublicInfo>('/public/config', { signal, cache: 'no-store' }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
    retry: false,
  })
  const [manifest, setManifest] = useState('')
  const manifestError = preferenceManifestError(manifest, installationOptions, preferences)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    request.current?.abort()
    request.current = null
    setPending(false)
    setManifest('')
    setError(null)
  }, [location.pathname])
  useEffect(() => {
    if (!info.data) return
    const allowed = new Set(
      (info.data.sources ?? info.data.private_sources ?? []).map((source) => source.id),
    )
    const selected = installationOptions.sources ?? []
    const sources = selected.filter((id) => allowed.has(id))
    if (selected.length !== sources.length) {
      const next = { ...installationOptions, sources }
      setInstallationOptions(next)
      setManifest((current) => (current ? preferenceManifest(current, preferences, next) : ''))
    }
  }, [info.data, installationOptions, preferences])
  useEffect(() => {
    if (info.data && !info.data.enabled) {
      setManifest('')
      request.current?.abort()
      setPending(false)
    }
  }, [info.data?.enabled])
  async function install(form: HTMLFormElement) {
    if (request.current) return
    const values = new FormData(form),
      services: Record<string, string> = {},
      announces: Record<string, string> = {}
    for (const provider of info.data?.providers ?? []) {
      const key = String(values.get(provider.id) ?? '').trim()
      if (key) services[provider.id] = key
    }
    if (!Object.keys(services).length) {
      setError(new Error('Enter at least one service key.'))
      return
    }
    for (const source of info.data?.private_sources ?? []) {
      const announce = String(values.get(`announce:${source.id}`) ?? '').trim()
      if (announce) announces[source.id] = announce
    }
    setPending(true)
    setError(null)
    setManifest('')
    const controller = new AbortController()
    request.current = controller
    try {
      // Credential values never enter React Query, navigation state or browser storage.
      const body = await api<{ manifest_url: string }>('/public/install', {
        method: 'POST',
        cache: 'no-store',
        body: JSON.stringify({
          services,
          preferences: {
            ...preferences,
            language: resolvedLanguage(preferences),
            ...installationOptions,
          },
          ...(Object.keys(announces).length ? { announces } : {}),
        }),
        signal: controller.signal,
      })
      if (!controller.signal.aborted) setManifest(body.manifest_url)
    } catch (caught) {
      if (!controller.signal.aborted) setError(caught)
    } finally {
      for (const name of Object.keys(services)) delete services[name]
      for (const name of Object.keys(announces)) delete announces[name]
      for (const name of [...values.keys()]) values.delete(name)
      clearPublicCredentials(form)
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  if (info.isPending)
    return (
      <main className="mx-auto max-w-sm px-6 py-24">
        <Loading label="Checking public access" />
      </main>
    )
  if (info.error && !info.data)
    return (
      <main className="mx-auto max-w-md px-6 py-24">
        <ErrorNotice error={info.error} retry={() => void info.refetch()} />
      </main>
    )
  if (!info.data?.enabled) return <Login />
  if (location.pathname === '/public/torznab')
    return layout(<PublicTorznabEntry info={info.data} />)
  return layout(
    <div className="page max-w-4xl">
      <section className="section-card">
        <h2 className="section-title">Connect your debrid services</h2>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void install(event.currentTarget)
          }}
        >
          <fieldset disabled={pending} className="space-y-5">
            {(info.data.providers ?? []).map((provider) => (
              <div className="field" key={provider.id}>
                <Label htmlFor={`public-${provider.id}`}>{provider.name} API key</Label>
                <Input
                  id={`public-${provider.id}`}
                  name={provider.id}
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  autoCapitalize="off"
                  maxLength={512}
                  disabled={pending}
                />
              </div>
            ))}
            <p className="help">
              Enter at least one key. The host processes your keys without saving them. Request
              history retains media, results and errors for up to 7 days without identifying you.
            </p>
            <InstallationFilters
              value={installationOptions}
              onChange={(next) => {
                setInstallationOptions(next)
                setManifest((current) =>
                  current ? preferenceManifest(current, preferences, next) : '',
                )
              }}
              sources={info.data.sources ?? info.data.private_sources ?? []}
              services={info.data.providers ?? []}
            >
              <LanguageFilter
                layout="stacked"
                value={preferences}
                onChange={(next) => {
                  setPreferences(next)
                  setManifest((current) =>
                    current ? preferenceManifest(current, next, installationOptions) : '',
                  )
                }}
              />
            </InstallationFilters>
            <PublicAnnounceFields sources={info.data.private_sources ?? []} pending={pending} />
          </fieldset>
          <ErrorNotice error={error || info.error} />
          {manifestError && (
            <p className="text-sm text-destructive" role="alert">
              {manifestError}
            </p>
          )}
          <Button type="submit" disabled={pending || Boolean(manifestError)}>
            {pending ? 'Checking services…' : 'Create installation link'}
          </Button>
        </form>
      </section>
      {manifest && !manifestError && (
        <section className="section-card">
          <div className="field">
            <Label htmlFor="public-manifest">Manifest URL</Label>
            <Input id="public-manifest" value={manifest} readOnly />
          </div>
          <p className="help">
            This encrypted link gives access to your service configuration. Keep it private.
          </p>
          <div className="flex flex-wrap gap-3">
            <CopyButton value={manifest} label="Copy manifest URL" variant="default" />
            <Button variant="outline" asChild>
              <a href={`stremio://${manifest.replace(/^https?:\/\//, '')}`}>Install in Stremio</a>
            </Button>
          </div>
        </section>
      )}
    </div>,
  )
}
