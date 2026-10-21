import { LanguageFilter } from '@/components/language-filter'
import {
  useLanguagePreferences,
  resolvedLanguage,
  preferenceManifest,
  preferenceManifestError,
} from '@/lib/language-preferences'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useLocation } from 'react-router-dom'
import { api, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice, Loading } from '@/components/common'
import { CopyButton } from '@/components/copy-button'
import { Login } from '@/components/login'
import { PublicAnnounceFields, type PublicInfo } from '@/components/public-connection-fields'
import { PublicTorznabEntry } from '@/components/public-torznab-entry'
import { PublicCredentialsForm } from '@/components/public-credentials-form'
import { PublicSearchEntry } from '@/components/public-search-entry'
import { usePublicCredential } from '@/lib/public-credentials'
import { InstallationFilters } from '@/components/installation-filters'
import type { InstallationOptions } from '@/lib/language-preferences'

export function PublicEntry({
  layout = (content) => content,
}: {
  layout?: (content: ReactNode) => ReactNode
}) {
  const location = useLocation()
  const info = useQuery({
    queryKey: ['public-config'],
    queryFn: ({ signal }) => api<PublicInfo>('/public/config', { signal, cache: 'no-store' }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
    retry: false,
  })
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
  const section = location.pathname.startsWith('/public/search')
    ? 'search'
    : location.pathname === '/public/torznab'
      ? 'torznab'
      : 'stremio'
  const available = {
    search: info.data.search_enabled,
    stremio: info.data.stremio_enabled,
    torznab: info.data.torznab_enabled,
  }
  if (!available[section]) {
    const target = available.stremio
      ? '/public'
      : available.search
        ? '/public/search'
        : available.torznab
          ? '/public/torznab'
          : ''
    return target ? <Navigate to={target} replace /> : <Login />
  }
  if (section === 'search') return layout(<PublicSearchEntry info={info.data} />)
  if (section === 'torznab') return layout(<PublicTorznabEntry info={info.data} />)
  return layout(<PublicStremioEntry info={info.data} />)
}

function PublicStremioEntry({ info }: { info: PublicInfo }) {
  const location = useLocation()
  const credential = usePublicCredential()
  const [preferences, setPreferences] = useLanguagePreferences()
  const [installationOptions, setInstallationOptions] = useState<InstallationOptions>({})
  const [manifest, setManifest] = useState('')
  const manifestCredential = useRef('')
  const manifestError = preferenceManifestError(manifest, installationOptions, preferences)
  useEffect(() => {
    setManifest('')
  }, [location.pathname])
  useEffect(() => {
    if (credential?.credential !== manifestCredential.current) setManifest('')
  }, [credential])
  useEffect(() => {
    const allowed = new Set((info.sources ?? info.private_sources ?? []).map((source) => source.id))
    const selected = installationOptions.sources ?? []
    const sources = selected.filter((id) => allowed.has(id))
    if (selected.length !== sources.length) {
      const next = { ...installationOptions, sources }
      setInstallationOptions(next)
      setManifest((current) => (current ? preferenceManifest(current, preferences, next) : ''))
    }
  }, [info, installationOptions, preferences])
  return (
    <div className="page max-w-4xl">
      <section className="section-card">
        <h2 className="section-title">Connect your debrid services</h2>
        <PublicCredentialsForm
          info={info}
          submitLabel="Create installation link"
          preferences={{
            ...preferences,
            language: resolvedLanguage(preferences),
            ...installationOptions,
          }}
          disabled={Boolean(manifestError)}
          onStart={() => setManifest('')}
          onConfigured={(result) => {
            manifestCredential.current = result.credential
            setManifest(result.manifest_url)
          }}
        >
          <InstallationFilters
            value={installationOptions}
            onChange={(next) => {
              setInstallationOptions(next)
              setManifest((current) =>
                current ? preferenceManifest(current, preferences, next) : '',
              )
            }}
            sources={info.sources ?? info.private_sources ?? []}
            services={info.providers ?? []}
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
          <PublicAnnounceFields sources={info.private_sources ?? []} pending={false} />
          {manifestError && (
            <p className="text-sm text-destructive" role="alert">
              {manifestError}
            </p>
          )}
        </PublicCredentialsForm>
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
    </div>
  )
}
