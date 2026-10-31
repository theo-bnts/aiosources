import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import type { CatalogEntities } from '@/lib/catalog-types'
import type { PublicInfo } from '@/components/public-connection-fields'
import { CatalogDataControls, CatalogPublishedFields } from '@/components/catalog-data-controls'
import { SourceFilter } from '@/components/source-filter'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/copy-button'
import { ErrorNotice } from '@/components/common'

type Options = { sources?: string[]; fields?: string[]; catalog_entities?: CatalogEntities }
type Connection = { url: string; password: string }

export function PublicSharedCatalogEntry({ info }: { info: PublicInfo }) {
  const [options, setOptions] = useState<Options>({})
  const [connection, setConnection] = useState<Connection>()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>()
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  function change(next: Options) {
    setOptions(next)
    setConnection(undefined)
    setError(undefined)
  }
  async function create() {
    if (request.current) return
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(undefined)
    setConnection(undefined)
    try {
      const result = await api<Connection>('/public/catalogs/install', {
        method: 'POST',
        cache: 'no-store',
        body: JSON.stringify(options),
        signal: controller.signal,
      })
      if (!controller.signal.aborted) setConnection(result)
    } catch (error) {
      if (!controller.signal.aborted) setError(error)
    } finally {
      if (request.current === controller) request.current = null
      if (!controller.signal.aborted) setPending(false)
    }
  }
  const fields = info.catalog_fields ?? []
  const selectedFields = options.fields ?? fields
  return (
    <div className="page max-w-4xl">
      <section className="section-card">
        <h2 className="section-title">Connect another AIOSources instance</h2>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void create()
          }}
        >
          <fieldset disabled={pending} className="min-w-0 space-y-5">
            <Disclosure title="Filters">
              <SourceFilter
                id="public-catalog-sources"
                value={options.sources ?? []}
                sources={info.catalog_sources ?? []}
                onChange={(sources) => change({ ...options, sources })}
              />
            </Disclosure>
            <CatalogDataControls
              id="public-catalog-data"
              value={options.catalog_entities ?? {}}
              allowed={info.catalog_entities ?? {}}
              onChange={(catalog_entities) => change({ ...options, catalog_entities })}
              allowAvailability={selectedFields.includes('info_hash')}
              publishedFields={
                <CatalogPublishedFields
                  id="public-catalog-fields"
                  fields={fields}
                  value={selectedFields}
                  onChange={(fields) =>
                    change({
                      ...options,
                      fields,
                      catalog_entities: {
                        ...options.catalog_entities,
                        availability:
                          options.catalog_entities?.availability && fields.includes('info_hash'),
                      },
                    })
                  }
                />
              }
            />
          </fieldset>
          <p className="help">
            Copy the catalog to your own instance and keep it up to date automatically. Playback
            uses your own accounts and private-source announce URLs. No visitor account is created.
          </p>
          <ErrorNotice error={error} />
          <Button type="submit" disabled={pending}>
            {pending ? 'Creating connection…' : 'Create connection link'}
          </Button>
        </form>
      </section>
      {connection && (
        <section className="section-card">
          <h2 className="section-title">Connect your instance</h2>
          <p className="help">
            In AIOSources, open Sources → Remote catalogs and add a remote catalog. Paste this URL
            and save the sharing password in Secrets. Enable the connection to start syncing.
          </p>
          <div className="field">
            <Label htmlFor="public-catalog-url">Catalog URL</Label>
            <Input
              id="public-catalog-url"
              value={connection.url}
              readOnly
              autoComplete="off"
              spellCheck={false}
            />
            <CopyButton value={connection.url} label="Copy catalog URL" />
          </div>
          <div className="field">
            <Label htmlFor="public-catalog-password">Sharing password</Label>
            <Input
              id="public-catalog-password"
              value={connection.password}
              readOnly
              type="password"
              revealLabel="sharing password"
              autoComplete="off"
              spellCheck={false}
            />
            <CopyButton
              value={connection.password}
              label="Copy sharing password"
              manualCopyMessage="Clipboard access is unavailable. Reveal the sharing password and copy it manually."
              successMessage="Copied to your clipboard. Keep it private."
            />
          </div>
          <p className="help">
            Keep the password with its matching URL. Changing data choices creates a separate
            connection. Turning off public sharing revokes existing links.
          </p>
        </section>
      )}
    </div>
  )
}
