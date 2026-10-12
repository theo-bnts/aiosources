import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError, queryClient, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
type Access = { revision: number; enabled: boolean }
export function PublicAccessSettings() {
  const query = useQuery({
    queryKey: ['public-access'],
    queryFn: ({ signal }) => api<Access>('/stremio/public-access', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const [draft, setDraft] = useState<Access>()
  const current = draft || query.data
  const dirty = Boolean(draft && draft.enabled !== query.data?.enabled)
  const save = useMutation({
    mutationFn: (value: Access) =>
      api('/stremio/public-access', { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async () => {
      setDraft(undefined)
      await queryClient.invalidateQueries({ queryKey: ['public-access'] })
      await queryClient.invalidateQueries({ queryKey: ['public-config'] })
    },
  })
  const review = useMutation({
    mutationFn: () => api<Access>('/stremio/public-access'),
    onSuccess: async (latest) => {
      await queryClient.cancelQueries({ queryKey: ['public-access'] })
      queryClient.setQueryData(['public-access'], latest)
      setDraft((current) => (current ? { ...latest, enabled: current.enabled } : undefined))
      save.reset()
    },
  })
  const conflict = Boolean(
    draft &&
    (draft.revision !== query.data?.revision ||
      (save.error instanceof ApiError && save.error.status === 409)),
  )
  const busy = save.isPending || review.isPending
  useUnsavedChanges(dirty, 'Public access', save.isPending)
  return (
    <form
      className="section-card"
      aria-label="Public access"
      onSubmit={(e) => {
        e.preventDefault()
        if (dirty && current && !conflict && !busy) save.mutate(current)
      }}
    >
      <h2 className="section-title">Public access</h2>
      <ErrorNotice error={query.error || save.error || review.error} />
      {query.isPending ? (
        <Loading />
      ) : (
        current && (
          <>
            <div className="flex items-center gap-3">
              <Switch
                id="public-access-enabled"
                checked={current.enabled}
                disabled={busy}
                onCheckedChange={(enabled) => {
                  setDraft({ ...(draft || query.data!), enabled })
                  save.reset()
                  review.reset()
                }}
              />
              <Label htmlFor="public-access-enabled">
                Allow visitors to use their own debrid accounts
              </Label>
            </div>
            <p className="help">
              Visitors receive an encrypted installation link. Keys are not saved, and
              administration stays protected. Turning this off revokes their links. AllDebrid uses
              your configured account to check missing cache availability.
            </p>
            {conflict && (
              <div className="notice notice-warning space-y-3" role="status">
                <p>
                  Public access was changed elsewhere. Review the latest settings before saving.
                </p>
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => review.mutate()}
                >
                  {review.isPending ? 'Loading latest settings…' : 'Load latest settings'}
                </Button>
              </div>
            )}
            <div className="flex flex-wrap items-center gap-3">
              <Button type="submit" disabled={!dirty || conflict || busy}>
                {save.isPending ? 'Saving…' : 'Save'}
              </Button>
              {dirty && (
                <Button
                  type="button"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => {
                    setDraft(undefined)
                    save.reset()
                    review.reset()
                  }}
                >
                  Discard changes
                </Button>
              )}
              {query.data?.enabled && (
                <Button variant="outline" asChild>
                  <Link to="/public">Open public homepage</Link>
                </Button>
              )}
              {(dirty || save.isPending || save.isSuccess) && (
                <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
              )}
            </div>
          </>
        )
      )}
    </form>
  )
}
