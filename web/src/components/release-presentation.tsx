import type { CatalogRelease } from '@/lib/catalog-search-types'

// Labels come from the same server formatter as Stremio. Keep the release
// information together and the per-service availability in the download column.
export function ReleasePresentation({ release }: { release: CatalogRelease }) {
  return (
    <div
      data-slot="release-presentation"
      className="min-w-0 space-y-1 text-sm leading-6 wrap-anywhere"
    >
      <p className="font-medium">{release.title}</p>
      {release.presentation?.language && <p>{release.presentation.language}</p>}
      {release.presentation?.details.map((line) => (
        <p key={line}>{line}</p>
      ))}
    </div>
  )
}
