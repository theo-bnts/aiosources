import { useId, useState } from 'react'
import { compareListNames } from '@/lib/list-view'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, queryClient } from '@/lib/api'
import type { Items, SecretInfo } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import { ErrorNotice } from '@/components/common'

// The value is held only by the open editor, never in query/mutation variables.
// Create a fresh reference; adding a credential cannot rotate someone else's secret.
export function SecretPicker({
  label,
  value,
  onChange,
  prefix,
  addLabel = 'Add credential',
  valueLabel = 'Credential value',
  emptyLabel = 'Not configured',
  required = false,
  disabled = false,
  inputId,
}: {
  label: string
  value: string
  onChange: (reference: string) => void
  prefix: string
  addLabel?: string
  valueLabel?: string
  emptyLabel?: string
  required?: boolean
  disabled?: boolean
  inputId?: string
}) {
  const generatedId = useId()
  const id = inputId ?? generatedId
  const [open, setOpen] = useState(false)
  const [secretValue, setSecretValue] = useState('')
  const [stored, setStored] = useState(false)
  const [reference, setReference] = useState('')
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
  })
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      await api<void>(`/secrets/${encodeURIComponent(reference)}`, {
        method: 'PUT',
        body: JSON.stringify({ value: secretValue, revision: 0 }),
      })
      return reference
    },
    onSuccess: async (reference) => {
      setSecretValue('')
      setOpen(false)
      setStored(true)
      onChange(reference)
      await queryClient.invalidateQueries({ queryKey: ['secrets'] })
    },
  })
  const entries = [...(secrets.data?.items ?? [])].sort(compareListNames)
  return (
    <div className="field min-w-0 grid-cols-1">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
        <select
          id={id}
          className="native-select min-w-0 w-full sm:flex-1"
          value={value}
          required={required}
          disabled={disabled || save.isPending}
          onChange={(event) => {
            setStored(false)
            onChange(event.target.value)
          }}
        >
          <option value="">{emptyLabel}</option>
          {value && !entries.some((secret) => secret.name === value) && (
            <option value={value}>{value} (current reference)</option>
          )}
          {entries.map((secret) => (
            <option key={secret.name} value={secret.name}>
              {secret.name}
            </option>
          ))}
        </select>
        <Button
          type="button"
          variant="outline"
          className="self-start"
          disabled={disabled || save.isPending || Boolean(value)}
          onClick={() => {
            const suffix = Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
              byte.toString(16).padStart(2, '0'),
            ).join('')
            setReference(`${prefix.replace(/[^a-zA-Z0-9_-]/g, '_').slice(0, 40)}_${suffix}`)
            save.reset()
            setSecretValue('')
            setOpen(true)
          }}
        >
          {addLabel}
        </Button>
      </div>
      {stored && (
        <p className="help" role="status">
          Stored securely and selected. The selection applies when this configuration is saved. The
          entry remains in Secrets if you discard these changes.
        </p>
      )}
      <ErrorNotice error={secrets.error} retry={() => void secrets.refetch()} />
      <Dialog
        open={open}
        unsavedChanges={{ dirty: secretValue !== '', label: 'new secret', pending: save.isPending }}
        onOpenChange={(next) => {
          if (!save.isPending) {
            setOpen(next)
            if (!next) {
              setSecretValue('')
              save.reset()
            }
          }
        }}
      >
        <DialogContent showCloseButton={!save.isPending}>
          <DialogHeader>
            <DialogTitle>{addLabel}</DialogTitle>
            <DialogDescription>
              Stored as a new encrypted entry in Secrets, then selected here. Existing credentials
              are preserved. Save the surrounding configuration to apply the selection.
            </DialogDescription>
          </DialogHeader>
          {open && (
            <form
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                event.stopPropagation()
                if (!save.isPending && secretValue) save.mutate()
              }}
            >
              <div className="field">
                <Label htmlFor={`${id}-value`}>{valueLabel}</Label>
                <Input
                  id={`${id}-value`}
                  type="password"
                  required
                  value={secretValue}
                  disabled={save.isPending}
                  onChange={(event) => setSecretValue(event.target.value)}
                  autoComplete="new-password"
                  spellCheck={false}
                />
              </div>
              <ErrorNotice error={save.error} />
              <DialogFooter>
                <DialogClose asChild>
                  <Button type="button" variant="outline" disabled={save.isPending}>
                    Cancel
                  </Button>
                </DialogClose>
                <Button type="submit" disabled={save.isPending || !secretValue}>
                  {save.isPending ? 'Storing…' : 'Store and select'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
