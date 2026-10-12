import { useEffect, useRef, useState } from 'react'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useQuery } from '@tanstack/react-query'
import { ShieldCheckIcon, SignOutIcon } from '@phosphor-icons/react'
import { api, queryClient, setCSRF, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { useDate } from '@/lib/format'
import type { Items } from '@/lib/types'
import type {
  AdminSession,
  MFAEnrollment,
  RecoveryCodes,
  SecurityStatus,
} from '@/lib/security-types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmAction, Empty, ErrorNotice, Loading } from '@/components/common'

type Operation = 'enroll' | 'confirm' | 'cancel' | 'disable' | 'recovery'
const titles: Record<Operation, string> = {
  enroll: 'Set up two-step verification',
  confirm: 'Confirm your authenticator',
  cancel: 'Cancel pending enrollment',
  disable: 'Disable two-step verification',
  recovery: 'Replace recovery codes',
}
export function SecurityPage({ view }: { view: 'mfa' | 'sessions' }) {
  const date = useDate()
  const status = useQuery({
    queryKey: ['security', 'status'],
    queryFn: ({ signal }) => api<SecurityStatus>('/security', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const sessions = useQuery({
    queryKey: ['security', 'sessions'],
    queryFn: ({ signal }) => api<Items<AdminSession>>('/security/sessions', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const sessionItems = sessions.data?.items ?? []
  const [operation, setOperation] = useState<Operation | null>(null)
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [notice, setNotice] = useState<{ message: string; warning?: boolean } | null>(null)
  const [enrollment, setEnrollment] = useState<MFAEnrollment | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)
  const [savedCodes, setSavedCodes] = useState(false)
  useUnsavedChanges(Boolean(recoveryCodes && !savedCodes), 'recovery codes')
  const request = useRef<AbortController | null>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  useEffect(() => {
    if (!enrollment) return
    const timeout = window.setTimeout(
      () => {
        setEnrollment(null)
        setNotice({
          message: 'The enrollment expired. Start setup again to obtain a new key.',
          warning: true,
        })
      },
      Math.max(0, new Date(enrollment.expires_at).getTime() - Date.now()),
    )
    return () => window.clearTimeout(timeout)
  }, [enrollment])

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: ['security'] })
  }
  function open(next: Operation) {
    returnFocus.current = document.activeElement as HTMLElement | null
    setOperation(next)
    setPassword('')
    setCode('')
    setError(null)
  }
  function close() {
    if (pending) return
    setOperation(null)
    setPassword('')
    setCode('')
    setError(null)
  }
  async function submit(credentials: FormData) {
    if (!operation || pending) return
    setPending(true)
    setError(null)
    const controller = new AbortController()
    request.current = controller
    const selected = operation
    try {
      // Only nonsensitive status/list data goes through TanStack Query. Setup
      // keys, password/factor input and one-time codes live only in this view.
      const result = await api<MFAEnrollment | RecoveryCodes | undefined>(
        `/security/mfa/${selected}`,
        {
          method: 'POST',
          body: JSON.stringify({
            password: String(credentials.get('password') ?? ''),
            code: String(credentials.get('code') ?? ''),
          }),
          signal: controller.signal,
        },
      )
      if (controller.signal.aborted) return
      setPassword('')
      setCode('')
      if (selected === 'enroll' && result && 'secret' in result) {
        setEnrollment(result)
        setOperation('confirm')
      } else {
        setOperation(null)
        setEnrollment(null)
        if (result && 'recovery_codes' in result) {
          setSavedCodes(false)
          setRecoveryCodes(result.recovery_codes)
        }
        setNotice({
          message:
            selected === 'cancel'
              ? 'Pending enrollment cancelled.'
              : selected === 'disable'
                ? 'Two-step verification disabled. All other sessions were revoked.'
                : 'Two-step verification updated. All other sessions were revoked.',
        })
      }
      void refresh()
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
  function signedOut() {
    setCSRF()
    window.dispatchEvent(new Event('session-expired'))
  }

  return (
    <div className="page">
      {notice && (
        <p className={`notice ${notice.warning ? 'notice-warning' : 'text-primary'}`} role="status">
          {notice.message}
        </p>
      )}
      {view === 'mfa' && (
        <section className="section-card" aria-label="Two-step verification">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <ShieldCheckIcon size={24} className="mt-0.5 shrink-0 text-primary" />
              <div>
                <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
                  Require a time-based authenticator code as well as your password. Recovery codes
                  let you regain access if your authenticator is unavailable.
                </p>
              </div>
            </div>
            {status.data && (
              <Badge variant={status.data.mfa_enabled ? 'success' : 'warning'}>
                {status.data.mfa_enabled ? 'Enabled' : 'Not enabled'}
              </Badge>
            )}
          </div>
          <ErrorNotice error={status.error} retry={() => void status.refetch()} />
          {status.isPending ? (
            <Loading label="Loading security settings" />
          ) : (
            status.data && (
              <>
                {status.data.mfa_enabled ? (
                  <>
                    <p className="text-sm">
                      <strong>{status.data.recovery_codes_remaining}</strong> unused recovery codes
                      remain. Each code works once.
                    </p>
                    {status.data.recovery_codes_remaining === 0 && (
                      <p className="notice notice-warning" role="status">
                        No recovery codes remain. Replace them now while you still have access to
                        your authenticator.
                      </p>
                    )}
                    <div className="toolbar">
                      <Button variant="outline" onClick={() => open('recovery')}>
                        Replace recovery codes
                      </Button>
                      <Button variant="outline" onClick={() => open('disable')}>
                        Disable verification
                      </Button>
                    </div>
                  </>
                ) : (
                  <div className="space-y-4">
                    {status.data.pending_expires_at && (
                      <p className="text-sm text-muted-foreground">
                        Setup is pending until {date(status.data.pending_expires_at)}.{' '}
                        {enrollment
                          ? 'Continue with the key already shown.'
                          : 'Start again for a new key if you closed the setup screen.'}
                      </p>
                    )}
                    <div className="toolbar">
                      <Button onClick={() => open(enrollment ? 'confirm' : 'enroll')}>
                        {enrollment
                          ? 'Continue setup'
                          : status.data.pending_expires_at
                            ? 'Restart setup'
                            : 'Set up verification'}
                      </Button>
                      {status.data.pending_expires_at && (
                        <Button variant="outline" onClick={() => open('cancel')}>
                          Cancel setup
                        </Button>
                      )}
                    </div>
                  </div>
                )}
                <p className="help">
                  Enabling, disabling or replacing recovery codes requires your current password and
                  revokes other sessions. Enabled protection can only be changed with an unused
                  authenticator or recovery code.
                </p>
              </>
            )
          )}
        </section>
      )}

      {view === 'sessions' && (
        <section className="space-y-4" aria-label="Active sessions">
          <div className="page-heading">
            <div>
              <p className="text-sm text-muted-foreground">
                Sessions expire after 12 hours. Device descriptions are deliberately coarse; no IP
                address or raw user agent is stored.
              </p>
            </div>
            <ConfirmAction
              title="Revoke all other sessions?"
              description="Every session except this browser will be signed out. Other browsers will need your password and, when enabled, a fresh verification code."
              label="Revoke others"
              destructive
              trigger={
                <Button variant="outline" disabled={!sessions.data || sessionItems.length < 2}>
                  Revoke others
                </Button>
              }
              action={async () => {
                await api<void>('/security/sessions/revoke-others', { method: 'POST' })
                setNotice({ message: 'All other sessions were revoked.' })
                await refresh()
              }}
            />
          </div>
          <ErrorNotice error={sessions.error} retry={() => void sessions.refetch()} />
          {sessions.isPending ? (
            <Loading label="Loading sessions" />
          ) : (
            sessions.data &&
            (sessionItems.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Device</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>Last seen</TableHead>
                      <TableHead>Expires</TableHead>
                      <TableHead className="text-right">Access</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sessionItems.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <div className="flex flex-wrap items-center gap-2">
                            <span>{item.user_agent}</span>
                            {item.current && <Badge variant="info">This session</Badge>}
                          </div>
                          <p className="mt-1 font-mono text-xs text-muted-foreground">
                            {item.id.slice(0, 12)}
                          </p>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(item.created_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(item.last_seen_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {date(item.expires_at)}
                        </TableCell>
                        <TableCell className="text-right">
                          <ConfirmAction
                            title={item.current ? 'Sign out this session?' : 'Revoke this session?'}
                            description={
                              item.current
                                ? 'You will be returned to sign-in immediately.'
                                : `The ${item.user_agent} session created ${date(item.created_at)} will lose access.`
                            }
                            label={item.current ? 'Sign out' : 'Revoke session'}
                            destructive
                            trigger={
                              <Button variant="ghost" size="sm">
                                <SignOutIcon />
                                {item.current ? 'Sign out' : 'Revoke'}
                              </Button>
                            }
                            action={async () => {
                              await api<void>(`/security/sessions/${item.id}`, { method: 'DELETE' })
                              if (item.current) signedOut()
                              else {
                                setNotice({ message: 'Session revoked.' })
                                await refresh()
                              }
                            }}
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <Empty
                title="No active sessions"
                description="Session status updates automatically."
              />
            ))
          )}
        </section>
      )}

      <Dialog
        open={operation !== null}
        unsavedChanges={{
          dirty: password !== '' || code !== '',
          label: 'security verification',
          pending,
        }}
        onOpenChange={(value) => {
          if (!value) close()
        }}
      >
        <DialogContent
          showCloseButton={!pending}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            returnFocus.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>{operation ? titles[operation] : 'Security verification'}</DialogTitle>
            <DialogDescription>
              {operation === 'enroll'
                ? 'Confirm your current password to generate a private authenticator key. Setup expires after ten minutes.'
                : operation === 'confirm'
                  ? 'Add the key to your authenticator, then confirm with your password and a fresh six-digit code. Other sessions will be revoked.'
                  : operation === 'cancel'
                    ? 'This invalidates the pending setup key. Confirm with your current password.'
                    : operation === 'disable'
                      ? 'This removes authenticator protection and all recovery codes. Other sessions will be revoked. Confirm with your password and an unused code.'
                      : 'All previous recovery codes will stop working and other sessions will be revoked. Confirm with your password and an unused code.'}
            </DialogDescription>
          </DialogHeader>
          {operation === 'confirm' && enrollment && (
            <div className="space-y-3 rounded-md border border-border p-4">
              <p className="text-sm font-medium">Manual authenticator setup</p>
              <dl className="space-y-2 text-xs">
                <div>
                  <dt className="text-muted-foreground">Account</dt>
                  <dd>AIOSources: Administrator</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Setup key</dt>
                  <dd className="mt-1 select-all break-all font-mono text-sm">
                    {enrollment.secret}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Settings</dt>
                  <dd>Time-based · SHA-1 · 6 digits · 30 seconds</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Expires</dt>
                  <dd>{date(enrollment.expires_at)}</dd>
                </div>
              </dl>
              <p className="help">
                Keep this key private. It is shown only for this pending setup and is not saved in
                browser storage.
              </p>
            </div>
          )}
          {operation === 'confirm' && !enrollment && (
            <p className="notice">
              Enter a code from the authenticator you already configured. If setup expired or you no
              longer have the key, close this dialog and start again.
            </p>
          )}
          <form
            className="space-y-5"
            onSubmit={(event) => {
              event.preventDefault()
              void submit(new FormData(event.currentTarget))
            }}
          >
            <div className="field">
              <Label htmlFor="security-password">Current password</Label>
              <Input
                id="security-password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                disabled={pending}
              />
            </div>
            {operation !== 'enroll' && operation !== 'cancel' && (
              <div className="field">
                <Label htmlFor="security-code">
                  {operation === 'confirm'
                    ? 'Authenticator code'
                    : 'Authenticator or recovery code'}
                </Label>
                <Input
                  id="security-code"
                  name="code"
                  type="text"
                  autoComplete="one-time-code"
                  autoCorrect="off"
                  inputMode={operation === 'confirm' ? 'numeric' : 'text'}
                  pattern={operation === 'confirm' ? '[0-9]{6}' : undefined}
                  autoCapitalize="off"
                  spellCheck={false}
                  maxLength={operation === 'confirm' ? 6 : 64}
                  required
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  disabled={pending}
                />
                <p className="help">
                  Codes are single-use. If you just signed in with an authenticator code, wait for
                  the next code.
                </p>
              </div>
            )}
            <ErrorNotice error={error} />
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline" disabled={pending}>
                  Back
                </Button>
              </DialogClose>
              <Button
                type="submit"
                variant={operation === 'disable' ? 'destructive' : 'default'}
                disabled={pending}
              >
                {pending
                  ? 'Verifying…'
                  : operation === 'enroll'
                    ? 'Generate key'
                    : operation === 'confirm'
                      ? 'Enable verification'
                      : operation === 'cancel'
                        ? 'Cancel enrollment'
                        : operation === 'disable'
                          ? 'Disable verification'
                          : 'Replace codes'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={recoveryCodes !== null}
        onOpenChange={(value) => {
          if (!value && savedCodes) setRecoveryCodes(null)
        }}
      >
        <DialogContent
          showCloseButton={savedCodes}
          onEscapeKeyDown={(event) => {
            if (!savedCodes) event.preventDefault()
          }}
          onPointerDownOutside={(event) => event.preventDefault()}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            returnFocus.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>Save your recovery codes</DialogTitle>
            <DialogDescription>
              These codes are shown once. Store them in a secure password manager or print a private
              copy. Each replaces one authenticator code but still requires your password.
            </DialogDescription>
          </DialogHeader>
          <div className="select-all space-y-2 rounded-md border border-border bg-muted/30 p-4 font-mono text-xs sm:text-sm">
            {recoveryCodes?.map((value) => (
              <p className="break-all" key={value}>
                {value}
              </p>
            ))}
          </div>
          <label className="flex items-start gap-3 text-sm">
            <input
              className="mt-1 accent-primary"
              type="checkbox"
              checked={savedCodes}
              onChange={(event) => setSavedCodes(event.target.checked)}
            />
            <span>
              I have saved these codes somewhere secure. I understand they cannot be displayed
              again.
            </span>
          </label>
          <DialogFooter>
            <Button disabled={!savedCodes} onClick={() => setRecoveryCodes(null)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
