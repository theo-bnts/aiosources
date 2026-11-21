export type MetadataSection = 'videos' | 'games' | 'music' | 'books' | 'identification'

export const metadataSections = {
  videos: { label: 'Videos', path: '/metadata/videos', service: 'tmdb' },
  games: { label: 'Games', path: '/metadata/games', service: 'igdb' },
  music: { label: 'Music', path: '/metadata/music', service: 'musicbrainz' },
  books: { label: 'Books', path: '/metadata/books', service: 'openlibrary' },
  identification: {
    label: 'Identification',
    path: '/metadata/identification',
    service: 'local',
  },
} as const

export const identificationResultsPath = `${metadataSections.identification.path}/results`
export const identificationCorrespondencesPath = `${metadataSections.identification.path}/correspondences`

export function metadataOperationSection(kind: string, service: string): MetadataSection {
  if (kind === 'matching') return 'identification'
  return service === 'igdb'
    ? 'games'
    : service === 'openlibrary'
      ? 'books'
      : service === 'musicbrainz'
        ? 'music'
        : 'videos'
}

export function metadataHistoryPath(section: MetadataSection) {
  return `${metadataSections[section].path}/history`
}

export function legacyMetadataHistory(search: string) {
  const query = new URLSearchParams(search)
  const section = metadataOperationSection(query.get('kind') || '', query.get('service') || '')
  query.delete('kind')
  query.delete('service')
  return metadataHistoryPath(section) + (query.size ? `?${query}` : '')
}
