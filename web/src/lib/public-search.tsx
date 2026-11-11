import { createContext, useContext } from 'react'
import type { PublicInfo } from '@/components/public-connection-fields'
import { api } from './api'
import { publicAPI } from './public-credentials'

export type PublicSearchAccess = {
  trusted?: boolean
  configuration: string
  credential: string
  scope: string
  info: PublicInfo
}
export const PublicSearchContext = createContext<PublicSearchAccess | null>(null)
export const usePublicSearch = () => useContext(PublicSearchContext)

export function visitorAPI<T>(path: string, visitor: PublicSearchAccess, init: RequestInit = {}) {
  return visitor.trusted
    ? api<T>(`/trusted${path}`, {
        ...init,
        headers: {
          ...Object.fromEntries(new Headers(init.headers)),
          ...(visitor.configuration ? { Authorization: `Bearer ${visitor.configuration}` } : {}),
        },
      })
    : publicAPI<T>(path, visitor.configuration, init, visitor.credential)
}
