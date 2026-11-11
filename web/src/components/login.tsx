import { useEffect, useRef, useState } from 'react'
import { DatabaseIcon } from '@phosphor-icons/react'
import { api, queryClient, setCSRF } from '@/lib/api'
import type { Session } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice } from '@/components/common'
import { Link, useNavigate } from 'react-router-dom'

export function Login({ trusted = false }: { trusted?: boolean }) {
  const navigate = useNavigate()
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])

  async function signIn(credentials: FormData) {
    if (pending) return
    setPending(true)
    setError(null)
    const controller = new AbortController()
    request.current = controller
    try {
      // Credentials never enter a query/mutation cache or browser storage.
      const value = await api<Session>(trusted ? '/trusted/login' : '/login', {
        method: 'POST',
        body: JSON.stringify({
          password: String(credentials.get('password') ?? ''),
          ...(trusted
            ? { email: String(credentials.get('email') ?? '').trim() }
            : { code: String(credentials.get('code') ?? '').trim() || undefined }),
        }),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      setPassword('')
      setCode('')
      await queryClient.cancelQueries()
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'session' })
      queryClient.getMutationCache().clear()
      setCSRF(value.csrf_token)
      queryClient.setQueryData(['session'], value)
      if (trusted) navigate('/trusted', { replace: true })
    } catch (caught) {
      if (!controller.signal.aborted) setError(caught)
    } finally {
      if (!controller.signal.aborted) {
        setPassword('')
        setCode('')
        setPending(false)
      }
    }
  }

  return (
    <main className="grid min-h-dvh place-items-center px-6 py-16">
      <section className="w-full max-w-sm">
        <div className="mb-10 flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-md border border-primary/25 bg-primary/10 text-primary">
            <DatabaseIcon size={22} />
          </div>
          <div>
            <p className="font-semibold tracking-tight">AIOSources</p>
            <p className="text-xs text-muted-foreground">
              {trusted ? 'Trusted user' : 'Administration'}
            </p>
          </div>
        </div>
        <h1 className="page-title">{trusted ? 'Trusted user sign-in' : 'Administrator sign-in'}</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          {trusted
            ? 'Use the email and password provided by the administrator. Contact them if you need a new password.'
            : 'Sign in with your administrator password. If two-step verification is enabled, an authenticator or recovery code is also required.'}
        </p>
        <form
          className="mt-8 space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void signIn(new FormData(event.currentTarget))
          }}
        >
          {trusted && (
            <div className="field">
              <Label htmlFor="login-email">Email</Label>
              <Input
                id="login-email"
                name="email"
                type="email"
                autoComplete="username"
                required
                autoFocus
                disabled={pending}
                maxLength={254}
              />
            </div>
          )}
          <div className="field">
            <Label htmlFor="password">{trusted ? 'Password' : 'Administrator password'}</Label>
            <Input
              id="password"
              name="password"
              type="password"
              revealLabel={trusted ? 'password' : 'administrator password'}
              autoComplete="current-password"
              autoFocus={!trusted}
              required
              value={password}
              disabled={pending}
              onChange={(event) => setPassword(event.target.value)}
            />
          </div>
          {!trusted && (
            <div className="field">
              <Label htmlFor="login-code">Authenticator or recovery code</Label>
              <Input
                id="login-code"
                name="code"
                type="text"
                autoComplete="one-time-code"
                autoCorrect="off"
                spellCheck={false}
                autoCapitalize="off"
                maxLength={64}
                value={code}
                disabled={pending}
                onChange={(event) => setCode(event.target.value)}
                aria-describedby="login-code-help"
              />
              <p id="login-code-help" className="help">
                Leave empty if two-step verification is not enabled. Each code can be used only
                once; wait for a new authenticator code after using one.
              </p>
            </div>
          )}
          <ErrorNotice error={error} />
          <Button className="w-full" disabled={pending} type="submit">
            {pending ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
        <Button variant="link" className="mt-4" asChild>
          <Link to={trusted ? '/login' : '/user-login'}>
            {trusted ? 'Administrator sign-in' : 'Trusted user sign-in'}
          </Link>
        </Button>
      </section>
    </main>
  )
}
