import { lazy, Suspense, useMemo } from 'react'
import { useLocation } from 'react-router-dom'
import { usePublicCredential } from '@/lib/public-credentials'
import { randomID } from '@/lib/random-id'
import { PublicSearchContext } from '@/lib/public-search'
import { Loading } from './common'
import { Disclosure } from './ui/disclosure'
import { PublicCredentialsForm } from './public-credentials-form'
import { PublicAnnounceFields, type PublicInfo } from './public-connection-fields'

const CatalogSearchPage = lazy(() =>
  import('@/pages/catalog-search').then((module) => ({ default: module.CatalogSearchPage })),
)
const CatalogTorrentsPage = lazy(() =>
  import('@/pages/catalog-torrents').then((module) => ({ default: module.CatalogTorrentsPage })),
)

const CatalogTitlePage = lazy(() =>
  import('@/pages/catalog-search').then((module) => ({ default: module.CatalogTitlePage })),
)

export function PublicSearchEntry({ info }: { info: PublicInfo }) {
  const saved = usePublicCredential(info.credential_scope)
  const location = useLocation()
  const configuration =
    saved && (saved.services.length || info.use_instance_debrid) ? saved.credential : ''
  const canSearch = Boolean(configuration || info.use_instance_debrid)
  const scope = useMemo(() => randomID(), [configuration])
  const base = info.trusted ? '/trusted' : '/public'
  const id = new RegExp(`^${base}/search/([0-9]+)$`).exec(location.pathname)?.[1]
  const form = (
    <PublicCredentialsForm
      info={info}
      connection="search"
      submitLabel={
        info.use_instance_debrid ? 'Save' : configuration ? 'Update services' : 'Start searching'
      }
    >
      <PublicAnnounceFields
        sources={info.private_sources ?? []}
        pending={false}
        scope={info.credential_scope}
      />
    </PublicCredentialsForm>
  )
  if (!canSearch)
    return (
      <div className="page max-w-4xl">
        <section className="section-card">
          <h2 className="section-title">Connect your debrid services</h2>
          {form}
        </section>
      </div>
    )
  return (
    <PublicSearchContext
      value={{
        trusted: info.trusted,
        configuration,
        credential: saved?.credential ?? '',
        scope,
        info,
      }}
    >
      <div className="page">
        {(!info.use_instance_debrid || Boolean(info.private_sources?.length)) && (
          <section className="section-card">
            {info.use_instance_debrid ? (
              form
            ) : (
              <Disclosure title="Debrid services">{form}</Disclosure>
            )}
          </section>
        )}
        <Suspense fallback={<Loading label="Loading Search" />}>
          {location.pathname === `${base}/search/torrents` ? (
            <CatalogTorrentsPage />
          ) : id ? (
            <CatalogTitlePage publicID={id} />
          ) : (
            <CatalogSearchPage />
          )}
        </Suspense>
      </div>
    </PublicSearchContext>
  )
}
