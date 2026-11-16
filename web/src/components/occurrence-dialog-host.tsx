import { useOccurrenceNavigation } from '@/lib/occurrence-navigation'
import { OccurrenceDialog } from '@/components/torrent-history'

export function OccurrenceDialogHost() {
  const navigation = useOccurrenceNavigation()
  if (!navigation.id) return null
  return (
    <OccurrenceDialog
      key={navigation.id}
      id={navigation.id}
      torrent={navigation.torrent}
      onClose={navigation.close}
      onOpen={(torrent, target) => navigation.open(torrent.occurrence_id, target, torrent)}
      onReturnFocus={navigation.returnFocus}
    />
  )
}
