import { useEffect, useRef, useState } from 'react'
import { DatabaseIcon } from '@phosphor-icons/react'
import { api, queryClient, setCSRF } from '@/lib/api'
import type { Session } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorNotice } from '@/components/common'

export function Login() {
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
      const value = await api<Session>('/login', {
        method: 'POST',
        body: JSON.stringify({
          password: String(credentials.get('password') ?? ''),
          code: String(credentials.get('code') ?? '').trim() || undefined,
        }),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      setPassword('')
      setCode('')
      setCSRF(value.csrf_token)
      queryClient.setQueryData(['session'], value)
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
            <p className="text-xs text-muted-foreground">Data administration</p>
          </div>
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Open a session</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          Sign in with your administrator password. If two-step verification is enabled, an
          authenticator or recovery code is also required.
        </p>
        <form
          className="mt-8 space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void signIn(new FormData(event.currentTarget))
          }}
        >
          <div className="field">
            <Label htmlFor="password">Administrator password</Label>
            <Input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              autoFocus
              required
              value={password}
              disabled={pending}
              onChange={(event) => setPassword(event.target.value)}
            />
          </div>
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
              Leave empty if two-step verification is not enabled. Each code can be used only once;
              wait for a new authenticator code after using one.
            </p>
          </div>
          <ErrorNotice error={error} />
          <Button className="w-full" disabled={pending} type="submit">
            {pending ? 'Signing in…' : 'Sign in'}
          </Button>
        </form>
      </section>
    </main>
  )
}
