import { useNotification } from '@/lib/notifications'
import { useQuery } from '@tanstack/react-query'
import { SignOutIcon } from '@phosphor-icons/react'
import { api, queryClient, setCSRF, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import { useDate } from '@/lib/format'
import type { Items } from '@/lib/types'
import type { AdminSession } from '@/lib/security-types'
import SecretsPage, { SecretReferencesHelp } from '@/pages/secrets'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { SettingsSections } from '@/components/settings-sections'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  ConfirmAction,
  Empty,
  ErrorNotice,
  Loading,
  PageHeaderVisibility,
  ResultCount,
} from '@/components/common'

export function SecuritySecretsPage() {
  return (
    <div className="page [&_.page]:p-0">
      <PageHeaderVisibility.Provider value={{ title: false, description: false }}>
        <section id="secrets" aria-label="Secrets" className="min-w-0 scroll-mt-20">
          <SecretsPage />
        </section>
        <SettingsSections>
          <SecretReferencesHelp />
        </SettingsSections>
      </PageHeaderVisibility.Provider>
    </div>
  )
}

export function SecuritySettingsPage() {
  return (
    <div className="page">
      <SettingsSections>
        <RuntimeSettingsPanel section="security" />
      </SettingsSections>
    </div>
  )
}
export function SecuritySessionsPage() {
  const date = useDate()
  const notify = useNotification()
  const sessions = useQuery({
    queryKey: ['security', 'sessions'],
    queryFn: ({ signal }) => api<Items<AdminSession>>('/security/sessions', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
  const sessionItems = sessions.data?.items ?? []
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['security'] })
  function signedOut() {
    setCSRF()
    window.dispatchEvent(new Event('session-expired'))
  }
  return (
    <div className="page" id="sessions">
      <section className="space-y-4" aria-label="Active sessions">
        <div className="page-heading">
          <div>
            <p className="mt-2 text-sm text-muted-foreground">
              Each session keeps the expiry assigned at sign-in. Device descriptions are
              deliberately coarse; no IP address or raw user agent is stored.
            </p>
          </div>
          <ConfirmAction
            title="Revoke all other sessions?"
            description="Every session except this browser will be signed out. Other browsers will need your password."
            label="Revoke others"
            variant="warning"
            trigger={
              <Button variant="warning" disabled={!sessions.data || sessionItems.length < 2}>
                Revoke others
              </Button>
            }
            action={async () => {
              await api<void>('/security/sessions/revoke-others', { method: 'POST' })
              notify({ message: 'All other sessions were revoked.' })
              await refresh()
            }}
          />
        </div>
        <ErrorNotice error={sessions.error} retry={() => void sessions.refetch()} />
        {sessions.data && <ResultCount total={sessionItems.length} label="sessions" />}
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
                          variant="warning"
                          trigger={
                            <Button variant="warning" size="sm">
                              <SignOutIcon />
                              {item.current ? 'Sign out' : 'Revoke'}
                            </Button>
                          }
                          action={async () => {
                            await api<void>(`/security/sessions/${item.id}`, { method: 'DELETE' })
                            if (item.current) signedOut()
                            else {
                              notify({ message: 'Session revoked.' })
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
            <Empty title="No active sessions" description="Session status updates automatically." />
          ))
        )}
      </section>
    </div>
  )
}
