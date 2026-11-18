import { TrashIcon, CheckIcon, EraserIcon } from '@phosphor-icons/react'
import { debridProviders } from '@/lib/debrid'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { api, ApiError } from '@/lib/api'
import {
  usePublicCredential,
  savePublicCredential,
  forgetPublicCredential,
  isCurrentPublicCredential,
  type PublicInstallation,
} from '@/lib/public-credentials'
import { clearPublicCredentials, type PublicInfo } from './public-connection-fields'
import { ErrorNotice } from './common'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { InfoTip } from './ui/info-tip'
import { CredentialPageLink } from './credential-page-link'
import type { LanguagePreferences, InstallationOptions } from '@/lib/language-preferences'

export function PublicCredentialsForm({
  info,
  connection = 'stremio',
  preferences,
  submitLabel,
  disabled,
  onConfigured,
  onStart,
  children,
}: {
  info: PublicInfo
  connection?: 'stremio' | 'search'
  preferences?: LanguagePreferences & InstallationOptions
  submitLabel: string
  disabled?: boolean
  onStart?: () => void
  onConfigured?: (value: PublicInstallation) => void
  children?: ReactNode
}) {
  const saved = usePublicCredential(info.credential_scope)
  const [removed, setRemoved] = useState<string[]>([])
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const request = useRef<AbortController | null>(null)
  const formRef = useRef<HTMLFormElement>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    const clear = () => {
      request.current?.abort()
      setPending(false)
      setError(null)
      if (formRef.current) clearPublicCredentials(formRef.current)
    }
    window.addEventListener('session-expired', clear)
    return () => window.removeEventListener('session-expired', clear)
  }, [])
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
    if (
      !info.use_instance_debrid &&
      !Object.keys(services).length &&
      !saved?.services.some((id) => !removed.includes(id))
    ) {
      setError(new Error('Enter at least one service key.'))
      return
    }
    for (const source of info.private_sources ?? []) {
      const announce = String(values.get(`announce:${source.id}`) ?? '').trim()
      if (announce) announces[source.id] = announce
    }
    const removedServices = removed.filter((id) => !services[id])
    const removedAnnounces = values
      .getAll('remove_announce')
      .map(String)
      .filter((id) => !announces[id])
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(null)
    onStart?.()
    try {
      const result = await api<PublicInstallation>(
        `/${info.trusted ? 'trusted' : 'public'}/${connection === 'search' ? 'credentials' : 'install'}`,
        {
          method: 'POST',
          cache: 'no-store',
          signal: controller.signal,
          body: JSON.stringify({
            services,
            ...(saved ? { credential: saved.credential } : {}),
            ...(removedServices.length ? { remove_services: removedServices } : {}),
            ...(removedAnnounces.length ? { remove_announces: removedAnnounces } : {}),
            ...(preferences ? { preferences } : {}),
            ...(Object.keys(announces).length ? { announces } : {}),
          }),
        },
      )
      if (
        !controller.signal.aborted &&
        isCurrentPublicCredential(saved?.credential, info.credential_scope)
      ) {
        savePublicCredential(result, info.credential_scope)
        onConfigured?.(result)
      }
    } catch (caught) {
      if (
        !controller.signal.aborted &&
        isCurrentPublicCredential(saved?.credential, info.credential_scope)
      ) {
        if (caught instanceof ApiError && caught.status === 401)
          forgetPublicCredential(saved?.credential, info.credential_scope)
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
        {!info.use_instance_debrid && (
          <div className="grid items-start gap-5 md:grid-cols-2">
            {!info.use_instance_debrid &&
              info.providers.map((provider) => {
                const details = debridProviders.find((item) => item.id === provider.id)
                const retained =
                  saved?.services.includes(provider.id) && !removed.includes(provider.id)
                return (
                  <div className="field" key={provider.id}>
                    <Label htmlFor={`public-${provider.id}`}>{provider.name} API key</Label>
                    <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
                      <Input
                        key={retained ? 'remembered' : 'new'}
                        id={`public-${provider.id}`}
                        name={provider.id}
                        type="password"
                        revealLabel={`${provider.name} API key`}
                        autoComplete="off"
                        spellCheck={false}
                        autoCapitalize="off"
                        maxLength={512}
                        autoFocus={removed.includes(provider.id)}
                        placeholder={
                          retained ? 'Saved on this browser · enter a key to replace it' : undefined
                        }
                      />
                      {retained &&
                        (saved?.services.filter((id) => !removed.includes(id)).length ?? 0) > 1 && (
                          <Button
                            type="button"
                            variant="warning"
                            size="sm"
                            className="self-start sm:self-auto"
                            onClick={() =>
                              setRemoved((ids) =>
                                ids.includes(provider.id) ? ids : [...ids, provider.id],
                              )
                            }
                          >
                            <TrashIcon aria-hidden="true" />
                            Remove key
                          </Button>
                        )}
                    </div>
                    {details && 'hint' in details && <p className="help">{details.hint}</p>}
                    {details && (
                      <CredentialPageLink href={details.credentialUrl}>
                        Get {provider.name} API key
                      </CredentialPageLink>
                    )}
                  </div>
                )
              })}
          </div>
        )}
        {!info.use_instance_debrid && (
          <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
            <p>
              Enter at least one key. Credentials are remembered on this browser, not saved on the
              server.
            </p>
            <InfoTip label="Personal credentials">
              <p>
                Your personal keys and optional announce URLs are remembered automatically on this
                browser in an encrypted configuration, shared between Search, Stremio and Torznab.
              </p>
              <p>
                Request history retains media, results and errors for the instance’s configured
                retention period without identifying you.
              </p>
            </InfoTip>
          </div>
        )}
        {children}
      </fieldset>
      {removed.length > 0 && <p className="help">Apply your changes to remove these saved keys.</p>}
      <ErrorNotice error={error} />
      <div className="flex flex-wrap gap-3">
        <Button type="submit" disabled={pending || disabled}>
          <CheckIcon aria-hidden="true" />
          {pending ? (info.use_instance_debrid ? 'Saving…' : 'Checking services…') : submitLabel}
        </Button>
        {saved && (
          <Button
            type="button"
            variant="warning"
            disabled={pending}
            onClick={() => {
              if (formRef.current) clearPublicCredentials(formRef.current)
              forgetPublicCredential(undefined, info.credential_scope)
              setError(null)
            }}
          >
            <EraserIcon aria-hidden="true" />
            Forget saved credentials
          </Button>
        )}
      </div>
    </form>
  )
}
