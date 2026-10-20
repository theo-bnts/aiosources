import { lazy, Suspense, useMemo, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { usePublicCredential } from '@/lib/public-credentials'
import { PublicSearchContext } from '@/lib/public-search'
import { Empty, Loading } from './common'
import { Button } from './ui/button'
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
  const [session, setSession] = useState<{ base: string; configuration: string } | null>(null)
  const configuration =
    saved && (session?.base === saved.credential ? session.configuration : saved.credential)
  const scope = useMemo(() => crypto.randomUUID(), [configuration])
  const id = /^\/public\/search\/([0-9]+)$/.exec(location.pathname)?.[1]
  if (!info.search_enabled)
    return (
      <div className="page">
        <Empty
          title="Public Search is disabled"
          description="You can still use the connections enabled on this instance."
        />
        <Button asChild variant="outline">
          <Link to="/public">Stremio</Link>
        </Button>
      </div>
    )
  const form = (
    <PublicCredentialsForm
      info={info}
      submitLabel={saved ? 'Update services' : 'Start searching'}
      onConfigured={(value, hasAnnounces) => {
        const token = new URL(value.manifest_url).pathname.split('/').at(-2) || ''
        setSession({
          base: value.credential,
          configuration: hasAnnounces ? token : value.credential,
        })
      }}
    >
      <PublicAnnounceFields sources={info.private_sources ?? []} pending={false} search />
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
