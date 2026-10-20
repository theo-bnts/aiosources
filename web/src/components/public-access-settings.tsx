import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError, queryClient, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'
import { Disclosure } from '@/components/ui/disclosure'
type Access = {
  revision: number
  enabled: boolean
  expose_sources: boolean
  torznab_enabled: boolean
  limits: { setup_per_minute: number; requests_per_minute: number; concurrent: number }
}
type Draft = { base: Access; value: Access }
export function PublicAccessSettings() {
  const query = useQuery({
    queryKey: ['public-access'],
    queryFn: ({ signal }) => api<Access>('/public-access', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const [draft, setDraft] = useState<Draft>()
  const current = draft?.value || query.data
  const dirty = Boolean(
    draft &&
    (draft.value.enabled !== query.data?.enabled ||
      draft.value.torznab_enabled !== query.data?.torznab_enabled ||
      draft.value.expose_sources !== query.data?.expose_sources ||
      JSON.stringify(draft.value.limits) !== JSON.stringify(query.data?.limits)),
  )
  function change(value: Partial<Omit<Access, 'revision'>>) {
    setDraft((draft) => ({
      base: draft?.base || query.data!,
      value: { ...(draft?.value || query.data!), ...value },
    }))
    save.reset()
    review.reset()
  }
  const save = useMutation({
    mutationFn: (value: Access) =>
      api('/public-access', { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async () => {
      setDraft(undefined)
      await queryClient.invalidateQueries({ queryKey: ['public-access'] })
      await queryClient.invalidateQueries({ queryKey: ['public-config'] })
    },
  })
  const review = useMutation({
    mutationFn: () => api<Access>('/public-access'),
    onSuccess: async (latest) => {
      await queryClient.cancelQueries({ queryKey: ['public-access'] })
      queryClient.setQueryData(['public-access'], latest)
      setDraft((draft) =>
        draft
          ? {
              base: latest,
              value: {
                ...latest,
                enabled:
                  draft.value.enabled !== draft.base.enabled ? draft.value.enabled : latest.enabled,
                torznab_enabled:
                  draft.value.torznab_enabled !== draft.base.torznab_enabled
                    ? draft.value.torznab_enabled
                    : latest.torznab_enabled,
                limits: Object.fromEntries(
                  Object.entries(latest.limits).map(([key, value]) => {
                    const field = key as keyof Access['limits']
                    return [
                      key,
                      draft.value.limits[field] !== draft.base.limits[field]
                        ? draft.value.limits[field]
                        : value,
                    ]
                  }),
                ) as Access['limits'],
                expose_sources:
                  draft.value.expose_sources !== draft.base.expose_sources
                    ? draft.value.expose_sources
                    : latest.expose_sources,
              },
            }
          : undefined,
      )
      save.reset()
    },
  })
  const conflict = Boolean(
    draft &&
    (draft.value.revision !== query.data?.revision ||
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
      <h2 className="section-title">Visitor connections</h2>
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
                onCheckedChange={(enabled) => change({ enabled })}
              />
              <Label htmlFor="public-access-enabled">Enable public access</Label>
            </div>
            <p className="help">
              Visitors receive an encrypted installation link. Keys are not saved, and
              administration stays protected. Turning this off revokes their links. AllDebrid uses
              your configured account to check missing cache availability.
            </p>
            <div className="flex items-center gap-3">
              <Switch
                id="public-torznab-enabled"
                checked={current.torznab_enabled}
                disabled={busy || !current.enabled}
                onCheckedChange={(torznab_enabled) => change({ torznab_enabled })}
              />
              <Label htmlFor="public-torznab-enabled">Allow public Torznab connections</Label>
            </div>
            <p className="help">
              Visitors can create encrypted Torznab configurations without an account. Disabling
              this option revokes those links.
            </p>
            <Disclosure title="Advanced">
              <div className="flex items-center gap-3">
                <Switch
                  id="public-private-downloads"
                  checked={current.expose_sources}
                  disabled={busy || !current.enabled}
                  onCheckedChange={(expose_sources) => change({ expose_sources })}
                />
                <Label htmlFor="public-private-downloads">Expose source names and filters</Label>
              </div>
              <p className="help">
                Applies to Stremio and Torznab. Visitors can select sources and supply their own
                announce URLs for private sources. Your source definitions and announce URLs stay
                private. When off, source names and source filters are hidden and private sources
                require known cached availability.
              </p>
            </Disclosure>
            <section className="space-y-4 border-t pt-5" aria-label="Public request limits">
              <h3 className="section-title">Request limits</h3>
              <div className="grid gap-5 md:grid-cols-3">
                {(
                  [
                    ['setup_per_minute', 'Link creations per minute', 100000],
                    ['requests_per_minute', 'Addon requests per minute', 100000],
                    ['concurrent', 'Simultaneous public requests', 1000],
                  ] as const
                ).map(([field, label, max]) => (
                  <div className="field min-w-0" key={field}>
                    <Label htmlFor={`public-limit-${field}`}>{label}</Label>
                    <Input
                      id={`public-limit-${field}`}
                      type="number"
                      min={1}
                      max={max}
                      step={1}
                      required
                      disabled={busy}
                      value={current.limits[field] || ''}
                      onChange={(event) =>
                        change({
                          limits: { ...current.limits, [field]: Number(event.target.value) },
                        })
                      }
                    />
                  </div>
                ))}
              </div>
              <p className="help">
                Per-minute limits apply to each visitor IP and are shared by Stremio and Torznab.
                Simultaneous requests are limited across this application process. Configure trusted
                incoming proxies in Network when using a reverse proxy.
              </p>
              <p className="help">
                Changes apply without restarting or replacing installation links. Lowering
                concurrency lets active requests finish before admitting new ones.
              </p>
            </section>
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
