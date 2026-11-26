import { createContext, useContext } from 'react'
import type { PublicInfo } from '@/components/public-connection-fields'
import { api, ApiError } from './api'
import { forgetPublicCredential } from './public-credentials'
import { routes } from './routes'

export type PublicSearchAccess = {
  trusted?: boolean
  configuration: string
  credential: string
  scope: string
  info: PublicInfo
}
export const PublicSearchContext = createContext<PublicSearchAccess | null>(null)
export const usePublicSearch = () => useContext(PublicSearchContext)

export function catalogSearchPath(visitor: PublicSearchAccess | null): string {
  return visitor?.trusted
    ? routes.trusted.search
    : visitor
      ? routes.public.search
      : routes.browse.search
}

export function catalogTrendingPath(visitor: PublicSearchAccess | null): string {
  return visitor?.trusted
    ? routes.trusted.trending
    : visitor
      ? routes.public.trending
      : routes.browse.trending
}

export function catalogQueryKey(
  visitor: PublicSearchAccess | null,
  resource: string,
  ...rest: unknown[]
): unknown[] {
  return visitor
    ? ['public-catalog', visitor.scope, resource, ...rest]
    : [`catalog-${resource}`, ...rest]
}

export async function visitorAPI<T>(
  path: string,
  visitor: PublicSearchAccess,
  init: RequestInit = {},
) {
  const headers = new Headers(init.headers)
  if (visitor.configuration) headers.set('Authorization', `Bearer ${visitor.configuration}`)
  try {
    return await api<T>(`/${visitor.trusted ? 'trusted' : 'public'}${path}`, {
      ...init,
      headers,
      cache: 'no-store',
    })
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.status === 401 &&
      visitor.credential &&
      (!visitor.trusted ||
        (!path.startsWith('/catalog/search') && !path.startsWith('/catalog/trending')))
    ) {
      forgetPublicCredential(visitor.credential, visitor.info.credential_scope)
    }
    throw error
  }
}

export function catalogAPI<T>(
  path: string,
  visitor: PublicSearchAccess | null,
  init: RequestInit = {},
) {
  return visitor ? visitorAPI<T>(path, visitor, init) : api<T>(path, init)
}
