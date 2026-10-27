import type { DebridService } from '@/lib/debrid'
export type CatalogTitle = {
  id: number
  type: 'movie' | 'tv' | 'book'
  cover?: string
  openlibrary?: string
  authors?: string[]
  title: string
  years: number[]
  tmdb: string
  imdb: string
  releases: number
  providers: string[]
  seasons: number[] | null
  selection: CatalogSelection
}
export type CatalogSelection = { season?: number; episode?: number; file?: string }
export type CatalogAvailability = {
  service: DebridService
  state: 'cached' | 'uncached' | 'unknown' | 'unavailable' | 'invalid'
  can_add: boolean
  file_count?: number
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
  files?: { path: string; size: number }[]
  url_valid_seconds?: number
  status: 'ready' | 'pending'
  url?: string
  operation_id: string
  preparation_id?: string
}
