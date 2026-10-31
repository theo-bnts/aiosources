import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, ApiError, queryClient, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import { catalogEntitiesKey, catalogEntityKeys, type CatalogEntities } from '@/lib/catalog-types'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { CatalogDataControls, CatalogPublishedFields } from '@/components/catalog-data-controls'
import { SourceFilter } from '@/components/source-filter'
import { Disclosure } from '@/components/ui/disclosure'
import { Button } from '@/components/ui/button'
import { ErrorNotice, Loading } from '@/components/common'
import { FormStatus } from '@/components/form-status'

type Policy = {
  revision: number
  sources?: string[]
  fields: string[]
  catalog_entities: CatalogEntities
}
type CatalogSettings = {
  policy: Policy
  fields: string[]
  sources: { id: string; name: string }[]
}
const selectionKey = (values?: string[]) => [...(values ?? [])].sort().join('\n')
function changed(value: Policy, base: Policy) {
  return (
    selectionKey(value.sources) !== selectionKey(base.sources) ||
    selectionKey(value.fields) !== selectionKey(base.fields) ||
    catalogEntitiesKey(value.catalog_entities) !== catalogEntitiesKey(base.catalog_entities)
  )
}

export function PublicCatalogSettings() {
  const query = useQuery({
    queryKey: ['public-catalog-policy'],
    queryFn: ({ signal }) => api<CatalogSettings>('/public-catalog', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const [draft, setDraft] = useState<{ base: Policy; value: Policy }>()
  const current = draft?.value ?? query.data?.policy
  const dirty = Boolean(draft && query.data && changed(draft.value, query.data.policy))
  const save = useMutation({
    mutationFn: (value: Policy) =>
      api('/public-catalog', { method: 'PUT', body: JSON.stringify(value) }),
    onSuccess: async () => {
      setDraft(undefined)
      await queryClient.invalidateQueries({ queryKey: ['public-catalog-policy'] })
      await queryClient.invalidateQueries({ queryKey: ['public-config'] })
    },
  })
  const review = useMutation({
    mutationFn: () => api<CatalogSettings>('/public-catalog'),
    onSuccess: async (latest) => {
      await queryClient.cancelQueries({ queryKey: ['public-catalog-policy'] })
      queryClient.setQueryData(['public-catalog-policy'], latest)
      setDraft((draft) => {
        if (!draft) return undefined
        const fields =
          selectionKey(draft.value.fields) !== selectionKey(draft.base.fields)
            ? draft.value.fields
            : latest.policy.fields
        const catalog_entities: CatalogEntities = Object.fromEntries(
          catalogEntityKeys.map((field) => [
            field,
            Boolean(draft.value.catalog_entities[field]) !==
            Boolean(draft.base.catalog_entities[field])
              ? draft.value.catalog_entities[field]
              : latest.policy.catalog_entities[field],
          ]),
        )
        catalog_entities.availability =
          Boolean(catalog_entities.availability) && fields.includes('info_hash')
        return {
          base: latest.policy,
          value: {
            ...latest.policy,
            sources:
              selectionKey(draft.value.sources) !== selectionKey(draft.base.sources)
                ? draft.value.sources
                : latest.policy.sources,
            fields,
            catalog_entities,
          },
        }
      })
      save.reset()
    },
  })
  function change(value: Partial<Policy>) {
    setDraft((draft) => ({
      base: draft?.base ?? query.data!.policy,
      value: { ...(draft?.value ?? query.data!.policy), ...value },
    }))
    save.reset()
    review.reset()
  }
  const busy = save.isPending || review.isPending
  const conflict = Boolean(
    draft &&
    (draft.value.revision !== query.data?.policy.revision ||
      (save.error instanceof ApiError && save.error.status === 409)),
  )
  useUnsavedChanges(dirty, 'Shared catalog data', save.isPending)
  return (
    <Disclosure title="Shared catalog data">
      <form
        className="space-y-5"
        aria-label="Shared catalog data"
        onSubmit={(event) => {
          event.preventDefault()
          if (dirty && current && !conflict && !busy) save.mutate(current)
        }}
      >
        <p className="help">
          Choose what visitors may copy. These permissions apply only to public shared catalogs.
          Visitors can select a smaller set of data.
        </p>
        <ErrorNotice error={query.error || save.error || review.error} />
        {query.isPending ? (
          <Loading />
        ) : (
          current &&
          query.data && (
            <>
              <fieldset disabled={busy} className="min-w-0 space-y-4">
                <Disclosure title="Filters">
                  <SourceFilter
                    id="public-catalog-allowed-sources"
                    sources={query.data.sources}
                    value={current.sources ?? []}
                    onChange={(sources) => change({ sources })}
                  />
                </Disclosure>
                <CatalogDataControls
                  id="public-catalog-allowed-data"
                  value={current.catalog_entities}
                  onChange={(catalog_entities) => change({ catalog_entities })}
                  allowAvailability={current.fields.includes('info_hash')}
                  publishedFields={
                    <CatalogPublishedFields
                      id="public-catalog-allowed-fields"
                      value={current.fields}
                      fields={query.data.fields}
                      onChange={(fields) =>
                        change({
                          fields,
                          catalog_entities: {
                            ...current.catalog_entities,
                            availability:
                              current.catalog_entities.availability && fields.includes('info_hash'),
                          },
                        })
                      }
                    />
                  }
                />
              </fieldset>
              {conflict && (
                <div className="notice notice-warning space-y-3" role="status">
                  <p>
                    Shared catalog data was changed elsewhere. Review the latest settings before
                    saving.
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
                {(dirty || save.isPending || save.isSuccess) && (
                  <FormStatus dirty={dirty} saving={save.isPending} saved={save.isSuccess} />
                )}
              </div>
            </>
          )
        )}
      </form>
    </Disclosure>
  )
}
