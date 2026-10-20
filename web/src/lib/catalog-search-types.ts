export type CatalogTitle = {
  id: number
  type: 'movie' | 'tv'
  title: string
  years: number[]
  tmdb: string
  imdb: string
  releases: number
  providers: string[]
  seasons: number[] | null
  selection: CatalogSelection
}
export type CatalogSelection = { season?: number; episode?: number }
export type CatalogAvailability = {
  service: 'alldebrid' | 'torbox'
  state: 'cached' | 'uncached' | 'unknown' | 'unavailable' | 'invalid'
  can_add: boolean
  filename?: string
  size?: number
}
export type CatalogRelease = {
  download_token?: string
  provider_name?: string
  fingerprint: string
  occurrence_id: string
  provider: string
  title: string
  size: number
  published_at?: string
  languages: string[]
  services: CatalogAvailability[]
}
export type CatalogDownload = {
  status: 'ready' | 'pending'
  url?: string
  operation_id: string
  preparation_id?: string
}
