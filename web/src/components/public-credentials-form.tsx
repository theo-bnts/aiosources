import { useEffect, useRef, useState, type ReactNode } from 'react'
import { api, ApiError } from '@/lib/api'
import {
  usePublicCredential,
  savePublicCredential,
  forgetPublicCredential,
  type PublicInstallation,
} from '@/lib/public-credentials'
import { clearPublicCredentials, type PublicInfo } from './public-connection-fields'
import { ErrorNotice } from './common'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import type { LanguagePreferences, InstallationOptions } from '@/lib/language-preferences'

export function PublicCredentialsForm({
  info,
  preferences,
  submitLabel,
  disabled,
  onConfigured,
  onStart,
  children,
}: {
  info: PublicInfo
  preferences?: LanguagePreferences & InstallationOptions
  submitLabel: string
  disabled?: boolean
  onStart?: () => void
  onConfigured: (value: PublicInstallation, hasAnnounces: boolean) => void
  children?: ReactNode
}) {
  const saved = usePublicCredential()
  const [removed, setRemoved] = useState<string[]>([])
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const request = useRef<AbortController | null>(null)
  const formRef = useRef<HTMLFormElement>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    setRemoved([])
  }, [saved])
  async function submit(form: HTMLFormElement) {
    if (request.current) return
    const values = new FormData(form),
      services: Record<string, string> = {},
      announces: Record<string, string> = {}
    for (const provider of info.providers) {
      const key = String(values.get(provider.id) ?? '').trim()
      if (key) services[provider.id] = key
    }
    if (!Object.keys(services).length && !saved?.services.some((id) => !removed.includes(id))) {
      setError(new Error('Enter at least one service key.'))
      return
    }
    for (const source of info.private_sources ?? []) {
      const announce = String(values.get(`announce:${source.id}`) ?? '').trim()
      if (announce) announces[source.id] = announce
    }
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(null)
    onStart?.()
    try {
      const result = await api<PublicInstallation>('/public/install', {
        method: 'POST',
        cache: 'no-store',
        signal: controller.signal,
        body: JSON.stringify({
          services,
          ...(saved ? { credential: saved.credential } : {}),
          ...(removed.length ? { remove_services: removed } : {}),
          ...(preferences ? { preferences } : {}),
          ...(Object.keys(announces).length ? { announces } : {}),
        }),
      })
      if (!controller.signal.aborted) {
        savePublicCredential(result)
        onConfigured(result, Object.keys(announces).length > 0)
      }
    } catch (caught) {
      if (!controller.signal.aborted) {
        if (caught instanceof ApiError && caught.status === 401)
          forgetPublicCredential(saved?.credential)
        setError(caught)
      }
    } finally {
      for (const key of Object.keys(services)) delete services[key]
      for (const key of Object.keys(announces)) delete announces[key]
      for (const key of [...values.keys()]) values.delete(key)
      clearPublicCredentials(form)
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  return (
    <form
      ref={formRef}
      className="space-y-5"
      onSubmit={(event) => {
        event.preventDefault()
        void submit(event.currentTarget)
      }}
    >
      <fieldset disabled={pending} className="space-y-5">
        {info.providers.map((provider) => {
          const retained = saved?.services.includes(provider.id) && !removed.includes(provider.id)
          return (
            <div className="field" key={provider.id}>
              <Label htmlFor={`public-${provider.id}`}>{provider.name} API key</Label>
              <div className="flex min-w-0 gap-2">
                <Input
                  id={`public-${provider.id}`}
                  name={provider.id}
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  autoCapitalize="off"
                  maxLength={512}
                  placeholder={
                    retained ? 'Saved on this browser · enter a key to replace it' : undefined
                  }
                />
                {retained && (saved?.services.length ?? 0) > 1 && (
                  <Button
                    type="button"
                    variant="warning"
                    size="sm"
                    onClick={() => setRemoved((ids) => [...ids, provider.id])}
                  >
                    Remove key
                  </Button>
                )}
              </div>
            </div>
          )
        })}
        <p className="help">
          Enter at least one key. Your keys are remembered automatically on this browser in an
          encrypted configuration, shared between Search and Stremio. The server does not save them.
          Request history retains media, results and errors for up to 7 days without identifying
          you.
        </p>
        {children}
      </fieldset>
      {removed.length > 0 && <p className="help">Apply your changes to remove these saved keys.</p>}
      <ErrorNotice error={error} />
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={pending || disabled}>
          {pending ? 'Checking services…' : submitLabel}
        </Button>
        {saved && (
          <Button
            type="button"
            variant="warning"
            disabled={pending}
            onClick={() => {
              if (formRef.current) clearPublicCredentials(formRef.current)
              forgetPublicCredential()
              setError(null)
            }}
          >
            Forget saved keys
          </Button>
        )}
      </div>
    </form>
  )
}
