import {
  ResultCount,
  ConfirmAction,
  Empty,
  ErrorNotice,
  Loading,
  PageHeader,
} from '@/components/common'
import { ListFilters, useListFilters } from '@/components/list-filters'
import { compareListNames, matchesListQuery } from '@/lib/list-view'
import { useNotification } from '@/lib/notifications'
import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { PlusIcon, KeyIcon, ArrowClockwiseIcon, TrashIcon } from '@phosphor-icons/react'
import { api, ApiError, queryClient } from '@/lib/api'
import { useDate } from '@/lib/format'
import type { Items, SecretInfo } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Disclosure } from '@/components/ui/disclosure'
import { FormStatus } from '@/components/form-status'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'

export default function SecretsPage() {
  const filters = useListFilters()
  const date = useDate()
  const returnFocus = useRef<HTMLElement | null>(null)
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
  })
  const items = [...(secrets.data?.items ?? [])].sort(compareListNames)
  const visible = items.filter((secret) => matchesListQuery(filters.query, secret.name))
  const filtered = Boolean(filters.query)
  const [editing, setEditing] = useState<{
    name: string
    rotate: boolean
    revision: number
  } | null>(null)
  const [name, setName] = useState('')
  const [value, setValue] = useState('')
  const notify = useNotification()
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      await api<void>(`/secrets/${encodeURIComponent(name)}`, {
        method: 'PUT',
        body: JSON.stringify({ value, revision: editing?.revision ?? 0 }),
      })
    },
    onSuccess: async () => {
      notify(
        editing?.rotate ? `The value of “${name}” was replaced.` : `Secret “${name}” was created.`,
      )
      setValue('')
      setEditing(null)
      await queryClient.invalidateQueries({ queryKey: ['secrets'] })
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409)
        void queryClient.invalidateQueries({ queryKey: ['secrets'] })
    },
  })
  const review = useMutation({
    mutationFn: async (secretName: string) => {
      const latest = await api<Items<SecretInfo>>('/secrets')
      const selected = latest.items?.find((secret) => secret.name === secretName)
      if (!selected)
        throw new ApiError(404, 'This secret no longer exists. Create a new reference instead.')
      return { latest, selected }
    },
    onSuccess: async ({ latest, selected }) => {
      await queryClient.cancelQueries({ queryKey: ['secrets'] })
      queryClient.setQueryData(['secrets'], latest)
      setEditing((current) =>
        current?.rotate && current.name === selected.name
          ? { ...current, revision: selected.revision }
          : current,
      )
      save.reset()
    },
  })
  const busy = save.isPending || review.isPending
  function openSecret(secretName = '') {
    returnFocus.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null
    setName(secretName)
    setValue('')
    save.reset()
    review.reset()
    setEditing({
      name: secretName,
      rotate: Boolean(secretName),
      revision: secrets.data?.items?.find((secret) => secret.name === secretName)?.revision ?? 0,
    })
  }
  return (
    <div className="page">
      <PageHeader
        title="Secrets"
        actions={
          <Button onClick={() => openSecret()}>
            <PlusIcon />
            Create secret
          </Button>
        }
      />
      <div className="notice flex items-start gap-3">
        <KeyIcon className="mt-1 shrink-0 text-primary" />
        <p>
          Create a secret here or directly from a settings form, then select its name where it is
          needed. Replacing a value updates every setting that uses this name. Values remain hidden
          after saving.
        </p>
      </div>
      <ErrorNotice error={secrets.error} retry={() => void secrets.refetch()} />
      {secrets.data &&
        (items.length || filtered ? (
          <ListFilters
            kind="secret"
            filters={filters}
            count={visible.length}
            total={items.length}
          />
        ) : (
          <ResultCount total={0} label="secrets" />
        ))}
      {secrets.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="table-frame">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Reference</TableHead>
                <TableHead>Last updated</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((secret) => (
                <TableRow key={secret.name}>
                  <TableCell className="font-mono text-xs">{secret.name}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {date(secret.updated_at)}
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-2">
                      <Button variant="warning" size="sm" onClick={() => openSecret(secret.name)}>
                        <ArrowClockwiseIcon />
                        Replace
                      </Button>
                      <ConfirmAction
                        title={`Delete secret “${secret.name}”?`}
                        description="The encrypted value will be permanently deleted. The server refuses deletion while a source or resumable run still references it."
                        label="Delete secret"
                        variant="destructive"
                        trigger={
                          <Button
                            variant="destructive"
                            size="sm"
                            aria-label={`Delete secret ${secret.name}`}
                          >
                            <TrashIcon />
                            Delete
                          </Button>
                        }
                        action={async () => {
                          await api<void>(`/secrets/${encodeURIComponent(secret.name)}`, {
                            method: 'DELETE',
                            body: JSON.stringify({ revision: secret.revision }),
                          })
                          notify(`Secret “${secret.name}” was deleted.`)
                          await queryClient.invalidateQueries({ queryKey: ['secrets'] })
                        }}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        !secrets.isError && (
          <Empty
            icon={KeyIcon}
            title={filtered ? 'No secrets match these filters' : 'The vault contains no secrets'}
            description={
              filtered
                ? 'Clear the filters to see your other secrets.'
                : 'Add the keys, passwords or cookies your sources need. The server never returns their values.'
            }
          />
        )
      )}
      <Dialog
        open={Boolean(editing)}
        unsavedChanges={{
          dirty: value !== '' || (editing !== null && name !== editing.name),
          label: 'secret',
          pending: busy,
        }}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setEditing(null)
            setValue('')
            save.reset()
          }
        }}
      >
        <DialogContent
          showCloseButton={!busy}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            returnFocus.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>{editing?.rotate ? 'Replace secret value' : 'Create secret'}</DialogTitle>
            <DialogDescription>
              {editing?.rotate
                ? `The current value of “${editing.name}” will be replaced. It cannot be retrieved from this console.`
                : 'The value is sent to the server once, then encrypted. It is not recorded in the browser or JSON definitions.'}
            </DialogDescription>
          </DialogHeader>
          <form
            className="space-y-5"
            autoComplete="off"
            onSubmit={(event) => {
              event.preventDefault()
              if (!busy) save.mutate()
            }}
          >
            <div className="field">
              <Label htmlFor="secret-name">Reference name</Label>
              <Input
                id="secret-name"
                required
                pattern="[A-Za-z0-9][A-Za-z0-9_-]*"
                maxLength={128}
                value={name}
                readOnly={editing?.rotate}
                disabled={busy}
                onChange={(event) => setName(event.target.value)}
                spellCheck={false}
                autoComplete="off"
              />
              <p className="help">
                Letters, digits, hyphens and underscores. Sources use this name as a reference.
              </p>
            </div>
            <div className="field">
              <Label htmlFor="secret-value">{editing?.rotate ? 'New value' : 'Secret value'}</Label>
              <Input
                id="secret-value"
                type="password"
                revealLabel={editing?.rotate ? 'new secret value' : 'secret value'}
                required
                value={value}
                disabled={busy}
                onChange={(event) => setValue(event.target.value)}
                autoComplete="new-password"
                spellCheck={false}
              />
              <p className="help">The value is never returned after saving.</p>
            </div>
            <ErrorNotice error={save.error} />
            <ErrorNotice error={review.error} />
            {save.error instanceof ApiError && save.error.status === 409 && (
              <div className="notice space-y-3">
                <p className="help">
                  {editing?.rotate
                    ? 'This secret changed elsewhere. Load its latest revision, then review before replacing it. Your new value will be kept.'
                    : 'This name already holds a different value. Your input has been kept. Choose an unused name, or cancel and use Replace on the existing entry.'}
                </p>
                {editing?.rotate && (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={busy}
                    onClick={() => review.mutate(editing.name)}
                  >
                    Load latest revision
                  </Button>
                )}
              </div>
            )}
            <FormStatus
              dirty={value !== '' || (editing !== null && name !== editing.name)}
              saving={save.isPending}
            />
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline" disabled={busy}>
                  Back
                </Button>
              </DialogClose>
              <Button
                type="submit"
                variant={editing?.rotate ? 'warning' : 'default'}
                disabled={busy || !value || !name}
              >
                {save.isPending
                  ? 'Saving…'
                  : editing?.rotate
                    ? 'Confirm replacement'
                    : 'Create secret'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export function SecretReferencesHelp() {
  return (
    <Disclosure id="secret-reference-help" tabIndex={-1} title="Using references in JSON">
      <p className="help">
        Reference stored names in <code className="mono">auth.secret_ref</code>,{' '}
        <code className="mono">auth.username_ref</code>,{' '}
        <code className="mono">auth.password_ref</code> or{' '}
        <code className="mono">http.secret_headers</code>. Keep secret values out of JSON
        definitions.
      </p>
    </Disclosure>
  )
}
