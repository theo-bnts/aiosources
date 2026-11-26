import { debridProviders } from './debrid'
import { routes } from './routes'

export type ActivityLevel = 'debug' | 'info' | 'warn' | 'error'
export const activityLevels: ActivityLevel[] = ['debug', 'info', 'warn', 'error']

export const activityServices = [
  { id: 'network', name: 'Network', to: routes.administration.network.incoming },
  { id: 'tmdb', name: 'TMDB', to: routes.metadata.catalogs.videos.statistics },
  { id: 'igdb', name: 'IGDB', to: routes.metadata.catalogs.games.statistics },
  { id: 'software', name: 'Software', to: routes.metadata.catalogs.software.statistics },
  { id: 'musicbrainz', name: 'MusicBrainz', to: routes.metadata.catalogs.music.statistics },
  { id: 'openlibrary', name: 'Open Library', to: routes.metadata.catalogs.books.statistics },
  ...debridProviders.map((provider) => ({ ...provider, to: routes.debrid.statistics })),
] as const

export interface LogEntry {
  id: number
  level: ActivityLevel
  kind: string
  title?: string
  message: string
  provider_id?: string
  service?: string
  run_id?: string
  created_at: string
  data?: Record<string, unknown>
}
