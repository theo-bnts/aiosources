import { useId, useState } from 'react'
import { compareListNames } from '@/lib/list-view'
import { randomID } from '@/lib/random-id'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, ApiError, queryClient } from '@/lib/api'
import { useNotification } from '@/lib/notifications'
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

type SecretEditor = { reference: string; revision: number; replacing: boolean }

async function latestSecret(reference: string) {
  const latest = await api<Items<SecretInfo>>('/secrets')
  const selected = latest.items?.find((secret) => secret.name === reference)
  if (!selected)
    throw new ApiError(404, 'This secret no longer exists. Select another entry or add a new one.')
  await queryClient.cancelQueries({ queryKey: ['secrets'] })
  queryClient.setQueryData(['secrets'], latest)
  return selected
}

// The value is held only by the open editor, never in query/mutation variables.
// Replacements retain the selected reference and require its observed revision.
export function SecretPicker({
  label,
  value,
  onChange,
  prefix,
  addLabel = 'Add credential',
  replaceLabel = addLabel.replace(/^Add /, 'Replace '),
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
  replaceLabel?: string
  valueLabel?: string
  emptyLabel?: string
  required?: boolean
  disabled?: boolean
  inputId?: string
}) {
  const generatedId = useId()
  const id = inputId ?? generatedId
  const [editor, setEditor] = useState<SecretEditor | null>(null)
  const [secretValue, setSecretValue] = useState('')
  const [stored, setStored] = useState<'created' | 'replaced' | null>(null)
  const notify = useNotification()
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
  })
  const save = useMutation({
    gcTime: 0,
    mutationFn: async () => {
      if (!editor) throw new ApiError(400, 'Open the credential editor before saving.')
      await api<void>(`/secrets/${encodeURIComponent(editor.reference)}`, {
        method: 'PUT',
        body: JSON.stringify({ value: secretValue, revision: editor.revision }),
      })
      return editor
    },
    onSuccess: async ({ reference, replacing }) => {
      setSecretValue('')
      setEditor(null)
      setStored(replacing ? 'replaced' : 'created')
      if (replacing) notify('Credential value replaced.')
      else onChange(reference)
      await queryClient.invalidateQueries({ queryKey: ['secrets'] })
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409)
        void queryClient.invalidateQueries({ queryKey: ['secrets'] })
    },
  })
  const prepare = useMutation({
    mutationFn: async (reference: string): Promise<SecretEditor> => {
      if (reference) {
        const selected = await latestSecret(reference)
        return { reference, revision: selected.revision, replacing: true }
      }
      return {
        reference: `${prefix.replace(/[^a-zA-Z0-9_-]/g, '_').slice(0, 40)}_${randomID()}`,
        revision: 0,
        replacing: false,
      }
    },
    onSuccess: (next) => {
      save.reset()
      review.reset()
      setSecretValue('')
      setEditor(next)
    },
  })
  const review = useMutation({
    mutationFn: latestSecret,
    onSuccess: (selected) => {
      setEditor((current) =>
        current?.replacing && current.reference === selected.name
          ? { ...current, revision: selected.revision }
          : current,
      )
      save.reset()
    },
  })
  const editorBusy = save.isPending || review.isPending
  const busy = editorBusy || prepare.isPending
  const conflict = save.error instanceof ApiError && save.error.status === 409
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
          disabled={disabled || busy}
          onChange={(event) => {
            setStored(null)
            prepare.reset()
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
          variant={value ? 'warning' : 'outline'}
          className="self-start"
          disabled={disabled || busy}
          onClick={() => prepare.mutate(value)}
        >
          {prepare.isPending ? 'Loading…' : value ? replaceLabel : addLabel}
        </Button>
      </div>
      {stored && (
        <p className="help" role="status">
          {stored === 'replaced'
            ? 'The encrypted value was replaced. Every configuration using this reference now uses the new value.'
            : 'Stored securely and selected. The selection applies when this configuration is saved. The entry remains in Secrets if you discard these changes.'}
        </p>
      )}
      <ErrorNotice error={secrets.error} retry={() => void secrets.refetch()} />
      <ErrorNotice error={prepare.error} scope="action" />
      <Dialog
        open={editor !== null}
        unsavedChanges={{
          dirty: secretValue !== '',
          label: editor?.replacing ? 'secret replacement' : 'new secret',
          pending: editorBusy,
        }}
        onOpenChange={(next) => {
          if (!editorBusy) {
            if (!next) {
              setEditor(null)
              setSecretValue('')
              save.reset()
              review.reset()
            }
          }
        }}
      >
        <DialogContent showCloseButton={!editorBusy}>
          <DialogHeader>
            <DialogTitle>{editor?.replacing ? replaceLabel : addLabel}</DialogTitle>
            <DialogDescription>
              {editor?.replacing
                ? `Replace the encrypted value of “${editor.reference}”. This applies immediately to every configuration using this reference. The current value remains hidden.`
                : 'Stored as a new encrypted entry in Secrets, then selected here. Existing credentials are preserved. Save the surrounding configuration to apply the selection.'}
            </DialogDescription>
          </DialogHeader>
          {editor && (
            <form
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                event.stopPropagation()
                if (!editorBusy && !conflict && secretValue) save.mutate()
              }}
            >
              <div className="field">
                <Label htmlFor={`${id}-value`}>{valueLabel}</Label>
                <Input
                  id={`${id}-value`}
                  type="password"
                  revealLabel={valueLabel}
                  required
                  value={secretValue}
                  disabled={editorBusy}
                  onChange={(event) => setSecretValue(event.target.value)}
                  autoComplete="new-password"
                  spellCheck={false}
                />
              </div>
              <ErrorNotice error={save.error} />
              <ErrorNotice error={review.error} />
              {conflict && (
                <div className="notice space-y-3">
                  <p className="help">
                    {editor.replacing
                      ? 'This secret changed elsewhere. Load its latest revision, then review before replacing it. Your new value will be kept.'
                      : 'This reference already exists. Close this editor and add a new credential to create a fresh reference.'}
                  </p>
                  {editor.replacing && (
                    <Button
                      type="button"
                      variant="outline"
                      disabled={editorBusy}
                      onClick={() => review.mutate(editor.reference)}
                    >
                      Load latest revision
                    </Button>
                  )}
                </div>
              )}
              <DialogFooter>
                <DialogClose asChild>
                  <Button type="button" variant="outline" disabled={editorBusy}>
                    Cancel
                  </Button>
                </DialogClose>
                <Button
                  type="submit"
                  variant={editor.replacing ? 'warning' : 'default'}
                  disabled={editorBusy || conflict || !secretValue}
                >
                  {save.isPending
                    ? 'Saving…'
                    : editor.replacing
                      ? 'Confirm replacement'
                      : 'Store and select'}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
