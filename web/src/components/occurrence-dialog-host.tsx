import { Suspense, lazy } from 'react'
import { useOccurrenceNavigation } from '@/lib/occurrence-navigation'

const OccurrenceDialog = lazy(() =>
  import('@/components/torrent-history').then((module) => ({
    default: module.OccurrenceDialog,
  })),
)

export function OccurrenceDialogHost() {
  const navigation = useOccurrenceNavigation()
  if (!navigation.id) return null
  return (
    <Suspense fallback={null}>
      <OccurrenceDialog
        id={navigation.id}
        torrent={navigation.torrent}
        onClose={navigation.close}
        onOpen={(torrent, target) => navigation.open(torrent.occurrence_id, target, torrent)}
        onReturnFocus={navigation.returnFocus}
      />
    </Suspense>
  )
}
