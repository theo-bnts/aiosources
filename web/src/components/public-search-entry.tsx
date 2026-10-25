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
const CatalogTitlePage = lazy(() =>
  import('@/pages/catalog-search').then((module) => ({ default: module.CatalogTitlePage })),
)

export function PublicSearchEntry({ info }: { info: PublicInfo }) {
  const saved = usePublicCredential()
  const location = useLocation()
  const configuration = saved?.services.length ? saved.credential : ''
  const scope = useMemo(() => randomID(), [configuration])
  const id = /^\/public\/search\/([0-9]+)$/.exec(location.pathname)?.[1]
  const form = (
    <PublicCredentialsForm
      info={info}
      connection="search"
      submitLabel={configuration ? 'Update services' : 'Start searching'}
    >
      <PublicAnnounceFields sources={info.private_sources ?? []} pending={false} />
    </PublicCredentialsForm>
  )
  if (!saved || !configuration)
    return (
      <div className="page max-w-4xl">
        <section className="section-card">
          <h2 className="section-title">Connect your debrid services</h2>
          {form}
        </section>
      </div>
    )
  return (
    <PublicSearchContext value={{ configuration, credential: saved.credential, scope, info }}>
      <div className="page">
        <section className="section-card">
          <Disclosure title="Debrid services">{form}</Disclosure>
        </section>
        <Suspense fallback={<Loading label="Loading Search" />}>
          {id ? <CatalogTitlePage publicID={id} /> : <CatalogSearchPage />}
        </Suspense>
      </div>
    </PublicSearchContext>
  )
}
