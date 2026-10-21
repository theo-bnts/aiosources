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
type Access = {
  revision: number
  enabled: boolean
  search_enabled: boolean
  stremio_enabled: boolean
  expose_sources: boolean
  torznab_enabled: boolean
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
      draft.value.search_enabled !== query.data?.search_enabled ||
      draft.value.stremio_enabled !== query.data?.stremio_enabled ||
      draft.value.torznab_enabled !== query.data?.torznab_enabled ||
      draft.value.expose_sources !== query.data?.expose_sources),
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
                search_enabled:
                  draft.value.search_enabled !== draft.base.search_enabled
                    ? draft.value.search_enabled
                    : latest.search_enabled,
                stremio_enabled:
                  draft.value.stremio_enabled !== draft.base.stremio_enabled
                    ? draft.value.stremio_enabled
                    : latest.stremio_enabled,
                torznab_enabled:
                  draft.value.torznab_enabled !== draft.base.torznab_enabled
                    ? draft.value.torznab_enabled
                    : latest.torznab_enabled,
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
      <ErrorNotice error={query.error || save.error || review.error} />
      {query.isPending ? (
        <Loading />
      ) : (
        current && (
          <>
            <div className="space-y-2">
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
                Visitors use the connections enabled below with their own keys. The server does not
                save these keys, and administration stays protected. Turning this off revokes public
                links. AllDebrid uses your configured account to check missing cache availability.
              </p>
            </div>
            <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
              <legend className="pr-3 text-sm font-medium">Connections</legend>
              <div className="space-y-2">
                <div className="flex items-center gap-3">
                  <Switch
                    id="public-search-enabled"
                    checked={current.search_enabled}
                    disabled={busy || !current.enabled}
                    onCheckedChange={(search_enabled) => change({ search_enabled })}
                  />
                  <Label htmlFor="public-search-enabled">Allow public Search</Label>
                </div>
                <p className="help">
                  Visitors can search the identified catalog and download using their own debrid
                  services. Source visibility and private download permissions follow the same rules
                  as Stremio. Browser downloads pass through this instance and use its bandwidth.
                </p>
              </div>
              <div className="space-y-2">
                <div className="flex items-center gap-3">
                  <Switch
                    id="public-stremio-enabled"
                    checked={current.stremio_enabled}
                    disabled={busy || !current.enabled}
                    onCheckedChange={(stremio_enabled) => change({ stremio_enabled })}
                  />
                  <Label htmlFor="public-stremio-enabled">Allow public Stremio connections</Label>
                </div>
                <p className="help">
                  Visitors can create installation links for Stremio and AIOStreams. Disabling this
                  option revokes those links without affecting Search or Torznab.
                </p>
              </div>
              <div className="space-y-2">
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
              </div>
            </fieldset>
            <fieldset className="min-w-0 space-y-4 border-t border-border pt-5">
              <legend className="pr-3 text-sm font-medium">Sources and privacy</legend>
              <div className="space-y-2">
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
                  Applies to Search, Stremio and Torznab. Visitors can select sources and supply
                  their own announce URLs for private sources. Your source definitions and announce
                  URLs stay private. When off, source names and source filters are hidden and
                  private sources require known cached availability.
                </p>
              </div>
            </fieldset>
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
