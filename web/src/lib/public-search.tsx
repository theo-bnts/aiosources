import { createContext, useContext } from 'react'
import type { PublicInfo } from '@/components/public-connection-fields'
import { api, ApiError } from './api'
import { forgetPublicCredential, publicAPI } from './public-credentials'

export type PublicSearchAccess = {
  trusted?: boolean
  configuration: string
  credential: string
  scope: string
  info: PublicInfo
}
export const PublicSearchContext = createContext<PublicSearchAccess | null>(null)
export const usePublicSearch = () => useContext(PublicSearchContext)

export async function visitorAPI<T>(
  path: string,
  visitor: PublicSearchAccess,
  init: RequestInit = {},
) {
  if (!visitor.trusted) {
    return publicAPI<T>(
      path,
      visitor.configuration,
      init,
      visitor.credential,
      visitor.info.credential_scope,
    )
  }
  try {
    return await api<T>(`/trusted${path}`, {
      ...init,
      cache: 'no-store',
      headers: {
        ...Object.fromEntries(new Headers(init.headers)),
        ...(visitor.configuration ? { Authorization: `Bearer ${visitor.configuration}` } : {}),
      },
    })
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.status === 401 &&
      visitor.credential &&
      !path.startsWith('/catalog/search')
    ) {
      forgetPublicCredential(visitor.credential, visitor.info.credential_scope)
    }
    throw error
  }
}
