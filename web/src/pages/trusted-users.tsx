import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { PlusIcon, PencilSimpleIcon, TrashIcon } from '@phosphor-icons/react'
import { api, queryClient } from '@/lib/api'
import type { TrustedUser } from '@/lib/trusted-users'
import { trustedPermissionControls } from '@/lib/trusted-users'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Disclosure } from '@/components/ui/disclosure'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { ConfirmAction, Empty, ErrorNotice, Loading, ResultCount } from '@/components/common'
import { PageActions } from '@/components/page-actions'
import { AccessPermissions } from '@/components/access-permissions'
import { CredentialPolicyControls } from '@/components/credential-policy-controls'
import { PublicCatalogSettings } from '@/components/public-catalog-settings'

const emptyUser: TrustedUser = {
  id: '',
  email: '',
  enabled: false,
  revision: 0,
  created_at: '',
  updated_at: '',
  search_enabled: true,
  torznab_enabled: true,
  stremio_enabled: true,
  catalog_enabled: true,
  use_instance_debrid: true,
  use_instance_announces: true,
}

export default function TrustedUsersPage() {
  const query = useQuery({
    queryKey: ['trusted-users'],
    queryFn: ({ signal }) => api<{ items: TrustedUser[] }>('/trusted-users', { signal }),
  })
  const [editing, setEditing] = useState<TrustedUser | null>(null)
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['trusted-users'] })
  const toggle = useMutation({
    mutationFn: ({ user, enabled }: { user: TrustedUser; enabled: boolean }) =>
      api(`/trusted-users/${encodeURIComponent(user.id)}`, {
        method: 'PUT',
        body: JSON.stringify({ ...user, enabled }),
      }),
    onSuccess: refresh,
  })
  return (
    <div className="page">
      <PageActions>
        <Button onClick={() => setEditing({ ...emptyUser })}>
          <PlusIcon />
          Add user
        </Button>
      </PageActions>
      <ErrorNotice error={toggle.error} scope="action" />
      <ErrorNotice error={query.error} retry={() => void query.refetch()} />
      {query.isPending ? (
        <Loading label="Loading trusted users" />
      ) : (
        <>
          <ResultCount total={query.data?.items.length ?? 0} label="users" />
          {!query.data?.items.length ? (
            <Empty
              title="No trusted users"
              description="Create an account to let someone use your catalog and debrid services without administration access."
            />
          ) : (
            <div className="grid items-stretch gap-4 xl:grid-cols-2">
              {query.data.items.map((user) => (
                <section className="section-card" key={user.id}>
                  <div className="flex min-w-0 items-center justify-between gap-4">
                    <h2 className="section-title min-w-0 break-all">{user.email}</h2>
                    <Switch
                      aria-label={`Enable ${user.email}`}
                      checked={user.enabled}
                      disabled={toggle.isPending}
                      onCheckedChange={(enabled) => toggle.mutate({ user, enabled })}
                    />
                  </div>
                  <p className="help">
                    {trustedPermissionControls
                      .filter(([key]) => user[key])
                      .map(([, label]) =>
                        label
                          .replace(/^Allow /, '')
                          .replace(/ connections$/, '')
                          .replace(/^./, (value) => value.toUpperCase()),
                      )
                      .join(' · ') || 'No features enabled'}
                  </p>
                  <div className="flex flex-wrap gap-2">
                    <Button variant="outline" onClick={() => setEditing(user)}>
                      <PencilSimpleIcon />
                      Edit
                    </Button>
                    <ConfirmAction
                      title="Delete trusted user?"
                      description="This permanently deletes the account and revokes its sessions and connection links. Instance credentials and catalog data are retained."
                      label="Delete user"
                      variant="destructive"
                      action={async () => {
                        await api(`/trusted-users/${encodeURIComponent(user.id)}`, {
                          method: 'DELETE',
                          body: JSON.stringify({ revision: user.revision }),
                        })
                        await refresh()
                      }}
                      trigger={
                        <Button variant="destructive">
                          <TrashIcon />
                          Delete
                        </Button>
                      }
                    />
                  </div>
                </section>
              ))}
            </div>
          )}
        </>
      )}
      <PublicCatalogSettings />
      {editing && (
        <UserEditor
          key={`${editing.id}:${editing.revision}`}
          original={editing}
          close={() => setEditing(null)}
          saved={refresh}
        />
      )}
    </div>
  )
}

function UserEditor({
  original,
  close,
  saved,
}: {
  original: TrustedUser
  close: () => void
  saved: () => Promise<unknown>
}) {
  const [value, setValue] = useState(original)
  const [password, setPassword] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  const dirty =
    !original.id || password !== '' || JSON.stringify(value) !== JSON.stringify(original)
  const changed = password !== '' || JSON.stringify(value) !== JSON.stringify(original)
  async function save(form: FormData) {
    if (pending) return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(undefined)
    try {
      // Native form values support password managers. Credentials never enter
      // the query/mutation cache or browser storage.
      const proof = String(form.get('password') ?? '')
      await api<TrustedUser>(
        `/trusted-users${original.id ? '/' + encodeURIComponent(original.id) : ''}`,
        {
          method: original.id ? 'PUT' : 'POST',
          signal: controller.signal,
          body: JSON.stringify({
            email: String(form.get('email') ?? ''),
            enabled: value.enabled,
            revision: value.revision,
            search_enabled: value.search_enabled,
            stremio_enabled: value.stremio_enabled,
            torznab_enabled: value.torznab_enabled,
            catalog_enabled: value.catalog_enabled,
            use_instance_debrid: value.use_instance_debrid,
            use_instance_announces: value.use_instance_announces,
            ...(proof ? { password: proof } : {}),
          }),
        },
      )
      if (controller.signal.aborted) return
      setPassword('')
      await saved()
      close()
    } catch (error) {
      if (!controller.signal.aborted) setError(error)
    } finally {
      if (!controller.signal.aborted) {
        setPassword('')
        setPending(false)
      }
    }
  }
  return (
    <Dialog
      open
      unsavedChanges={{ dirty: changed, label: 'Trusted user', pending }}
      onOpenChange={(open) => {
        if (!open && !pending) close()
      }}
    >
      <DialogContent aria-describedby={undefined} showCloseButton={!pending}>
        <DialogHeader>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <DialogTitle>{original.id ? 'Edit trusted user' : 'Add trusted user'}</DialogTitle>
            <Switch
              aria-label="Enable account"
              checked={value.enabled}
              disabled={pending}
              onCheckedChange={(enabled) => setValue({ ...value, enabled })}
            />
          </div>
        </DialogHeader>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void save(new FormData(event.currentTarget))
          }}
        >
          <fieldset className="min-w-0 space-y-5" disabled={pending}>
            <div className="field">
              <Label htmlFor="trusted-email">Email</Label>
              <Input
                id="trusted-email"
                type="email"
                name="email"
                autoComplete="off"
                required
                maxLength={254}
                value={value.email}
                onChange={(e) => setValue({ ...value, email: e.target.value })}
              />
            </div>
            <div className="field">
              <Label
                htmlFor="trusted-password"
                help={
                  original.id
                    ? 'Leave empty to keep the current password. Replacing it revokes all sessions and connection links. There is no email recovery; the administrator sets a new password here.'
                    : 'Use at least 12 characters. Share the credentials with the user yourself; no email is sent.'
                }
              >
                {original.id ? 'Replace password' : 'Password'}
              </Label>
              <Input
                id="trusted-password"
                name="password"
                type="password"
                revealLabel="user password"
                autoComplete="new-password"
                required={!original.id}
                minLength={12}
                maxLength={1024}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <Disclosure
              title="Permissions"
              help="Accounts never have administration access. Saving changes revokes existing sessions and connection links; the user signs in again to create new links."
            >
              <AccessPermissions
                id="trusted"
                value={value}
                onChange={(change) => setValue({ ...value, ...change })}
              />
            </Disclosure>
            <Disclosure title="Credentials">
              <CredentialPolicyControls
                id="trusted-credentials"
                value={value}
                onChange={(change) => setValue({ ...value, ...change })}
              />
            </Disclosure>
          </fieldset>
          <ErrorNotice error={error} scope="action" />
          <DialogFooter>
            <Button type="submit" disabled={pending || !dirty}>
              {pending ? 'Saving…' : 'Save'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
