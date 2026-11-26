import { routes } from './routes.ts'

export type MetadataSection = 'videos' | 'games' | 'software' | 'music' | 'books' | 'identification'

export const metadataSections = {
  videos: {
    label: 'Videos',
    path: routes.metadata.catalogs.videos.base,
    statistics: routes.metadata.catalogs.videos.statistics,
    settings: routes.metadata.catalogs.videos.settings,
    history: routes.metadata.catalogs.videos.history,
    service: 'tmdb',
  },
  games: {
    label: 'Games',
    path: routes.metadata.catalogs.games.base,
    statistics: routes.metadata.catalogs.games.statistics,
    settings: routes.metadata.catalogs.games.settings,
    history: routes.metadata.catalogs.games.history,
    service: 'igdb',
  },
  software: {
    label: 'Software',
    path: routes.metadata.catalogs.software.base,
    statistics: routes.metadata.catalogs.software.statistics,
    settings: routes.metadata.catalogs.software.settings,
    history: routes.metadata.catalogs.software.history,
    service: 'software',
  },
  music: {
    label: 'Music',
    path: routes.metadata.catalogs.music.base,
    statistics: routes.metadata.catalogs.music.statistics,
    settings: routes.metadata.catalogs.music.settings,
    history: routes.metadata.catalogs.music.history,
    service: 'musicbrainz',
  },
  books: {
    label: 'Books',
    path: routes.metadata.catalogs.books.base,
    statistics: routes.metadata.catalogs.books.statistics,
    settings: routes.metadata.catalogs.books.settings,
    history: routes.metadata.catalogs.books.history,
    service: 'openlibrary',
  },
  identification: {
    label: 'Identification',
    path: routes.metadata.identification.base,
    statistics: routes.metadata.identification.statistics,
    settings: routes.metadata.identification.settings,
    history: routes.metadata.identification.history,
    service: 'local',
  },
} as const

export const identificationResultsPath = routes.metadata.identification.results
export const identificationCorrespondencesPath = routes.metadata.identification.correspondences

export function metadataOperationSection(kind: string, service: string): MetadataSection {
  if (kind === 'matching') return 'identification'
  return service === 'igdb'
    ? 'games'
    : service === 'software'
      ? 'software'
      : service === 'openlibrary'
        ? 'books'
        : service === 'musicbrainz'
          ? 'music'
          : 'videos'
}

export function metadataHistoryPath(section: MetadataSection) {
  return metadataSections[section].history
}

export function legacyMetadataHistory(search: string) {
  const query = new URLSearchParams(search)
  const section = metadataOperationSection(query.get('kind') || '', query.get('service') || '')
  query.delete('kind')
  query.delete('service')
  return metadataHistoryPath(section) + (query.size ? `?${query}` : '')
}
