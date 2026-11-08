import { Link } from 'react-router-dom'
import { usePublicSearch } from '@/lib/public-search'
import { ReleasePresentation } from '@/components/release-presentation'
import { DownloadAction } from '@/components/catalog-download'
import type { CatalogRelease, CatalogSelection } from '@/lib/catalog-search-types'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'

export function CatalogReleaseList({
  items,
  id,
  selection = {},
  discloseSources,
}: {
  items: CatalogRelease[]
  id: string
  selection?: CatalogSelection
  discloseSources: boolean
}) {
  const visitor = usePublicSearch()
  return (
    <div className="table-frame">
      <Table className="max-md:block max-md:[&_thead]:sr-only max-md:[&_tbody]:block max-md:[&_tr]:grid max-md:[&_tr]:grid-cols-1 max-md:[&_td]:block">
        <TableHeader>
          <TableRow>
            <TableHead>{discloseSources ? 'Version / source' : 'Version'}</TableHead>
            <TableHead>Download</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((release) => (
            <TableRow key={release.occurrence_id}>
              <TableCell className="max-w-xl align-top whitespace-normal">
                <ReleasePresentation release={release} />
              </TableCell>
              <TableCell className="align-top whitespace-normal">
                {release.services.length ? (
                  <div className="flex flex-wrap gap-5">
                    {release.services.map((service) => (
                      <DownloadAction
                        key={`${visitor?.scope ?? 'admin'}:${id}:${selection.season}:${selection.episode}:${release.occurrence_id}:${release.fingerprint}:${service.service}`}
                        workID={id}
                        release={release}
                        service={service}
                        selection={selection}
                      />
                    ))}
                  </div>
                ) : (
                  <p className="help">
                    {visitor ? (
                      'No service is available for this version.'
                    ) : (
                      <>
                        <Link to="/debrid/settings">Configure a debrid service</Link> to download
                        this version.
                      </>
                    )}
                  </p>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
