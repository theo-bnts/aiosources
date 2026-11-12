import { useEffect, useRef, useState } from 'react'
import { DatabaseIcon } from '@phosphor-icons/react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { api, queryClient, setCSRF } from '@/lib/api'
import type { Session } from '@/lib/types'
import { Button } from './ui/button'
import { Input } from './ui/input'
import { Label } from './ui/label'
import { Tabs, TabsList, TabsTrigger, TabsContent } from './ui/tabs'
import { ErrorNotice } from './common'

export function Login() {
  const [search, setSearch] = useSearchParams()
  const role = search.get('role') === 'administrator' ? 'administrator' : 'trusted'
  return (
    <main className="grid min-h-dvh place-items-center px-6 py-16">
      <section className="w-full max-w-sm space-y-8">
        <div className="flex items-center gap-3">
          <DatabaseIcon size={28} className="shrink-0 text-primary" aria-hidden="true" />
          <span className="font-semibold tracking-tight">AIOSources</span>
        </div>
        <Tabs
          value={role}
          onValueChange={(value) =>
            setSearch(value === 'administrator' ? { role: value } : {}, { replace: true })
          }
        >
          <TabsList aria-label="Login">
            <TabsTrigger value="trusted">Trusted user</TabsTrigger>
            <TabsTrigger value="administrator">Administrator</TabsTrigger>
          </TabsList>
          <TabsContent value={role} className="pt-5">
            <LoginForm key={role} trusted={role === 'trusted'} />
          </TabsContent>
        </Tabs>
      </section>
    </main>
  )
}

function LoginForm({ trusted }: { trusted: boolean }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [password, setPassword] = useState('')
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
          ...(trusted ? { email: String(credentials.get('email') ?? '').trim() } : {}),
        }),
        signal: controller.signal,
      })
      if (controller.signal.aborted) return
      setPassword('')
      await queryClient.cancelQueries()
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'session' })
      queryClient.getMutationCache().clear()
      setCSRF(value.csrf_token)
      queryClient.setQueryData(['session'], value)
      const query = new URLSearchParams(location.search)
      query.delete('role')
      const returnTo =
        !['/login', '/user-login', '/'].includes(location.pathname) &&
        !/^\/(public|trusted)(\/|$)/.test(location.pathname)
          ? location.pathname + (query.size ? '?' + query.toString() : '') + location.hash
          : '/providers'
      navigate(trusted ? '/trusted' : returnTo, { replace: true })
    } catch (caught) {
      if (!controller.signal.aborted) setError(caught)
    } finally {
      if (!controller.signal.aborted) {
        setPassword('')
        setPending(false)
      }
    }
  }

  return (
    <form
      className="space-y-5"
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
      <ErrorNotice error={error} />
      <Button className="w-full" disabled={pending} type="submit">
        {pending ? 'Signing in…' : 'Sign in'}
      </Button>
    </form>
  )
}
