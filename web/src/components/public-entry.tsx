import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { DatabaseIcon } from '@phosphor-icons/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice, Loading } from '@/components/common'
import { CopyButton } from '@/components/copy-button'
import { Login } from '@/components/login'

type PublicInfo = { enabled: boolean; providers: { id: string; name: string }[] }
export function PublicEntry({ authenticated = false }: { authenticated?: boolean }) {
  const info = useQuery({
    queryKey: ['public-config'],
    queryFn: async ({ signal }) => {
      // Access proxies need their same-origin session cookie even on public routes.
      const response = await fetch('/api/public/config', {
        signal,
        cache: 'no-store',
        credentials: 'same-origin',
      })
      if (!response.ok) throw new Error('Public configuration is unavailable.')
      return response.json() as Promise<PublicInfo>
    },
    refetchInterval: 5000,
    retry: false,
  })
  const [manifest, setManifest] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if (info.data && !info.data.enabled) {
      setManifest('')
      request.current?.abort()
      setPending(false)
    }
  }, [info.data?.enabled])
  async function install(form: HTMLFormElement) {
    if (pending) return
    const values = new FormData(form),
      services: Record<string, string> = {}
    for (const provider of info.data?.providers ?? []) {
      const key = String(values.get(provider.id) ?? '').trim()
      if (key) services[provider.id] = key
    }
    if (!Object.keys(services).length) {
      setError(new Error('Enter at least one service key.'))
      return
    }
    setPending(true)
    setError(null)
    setManifest('')
    const controller = new AbortController()
    request.current = controller
    try {
      // Credential values never enter React Query, navigation state or browser storage.
      const response = await fetch('/api/public/install', {
        method: 'POST',
        credentials: 'same-origin',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ services }),
        signal: controller.signal,
      })
      const body = await response.json()
      if (!response.ok) throw new Error(body.error || 'Could not create the installation link.')
      if (!controller.signal.aborted) setManifest(body.manifest_url)
    } catch (caught) {
      if (!controller.signal.aborted) setError(caught)
    } finally {
      for (const name of Object.keys(services)) delete services[name]
      form.reset()
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
  return (
    <main className="grid min-h-dvh place-items-center px-6 py-16">
      <section className="w-full max-w-md space-y-6">
        <div className="flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <DatabaseIcon size={24} className="text-primary" />
            <p className="font-semibold">AIOSources</p>
          </div>
          <Button variant="ghost" size="sm" asChild>
            <Link to={authenticated ? '/providers' : '/login'}>
              {authenticated ? 'Administration' : 'Admin login'}
            </Link>
          </Button>
        </div>
        <h1 className="page-title">Connect your debrid services</h1>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void install(event.currentTarget)
          }}
        >
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
          <ErrorNotice error={error || info.error} />
          <Button type="submit" disabled={pending}>
            {pending ? 'Checking services…' : 'Create installation link'}
          </Button>
        </form>
        {manifest && (
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
      </section>
    </main>
  )
}
